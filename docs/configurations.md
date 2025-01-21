# Supported PTP configurations
## Background 
### Linux synchronization software
- ptp4l implements the PTP protocol. It listens from time synchronization updates from a grandmaster to adjust a PHC (Precision Hardware Clock). The PHC is located in the NIC.
- ts2phc reads the timestamp from an external source to adjust the PHC. ts2phc reads the serial nmea output from the GNSS to get the current time and sets the PHC clock located on the NIC.
- phc2sys updates the system clock with the value of the PHC clock. Only one PHC should be synchronized to the system clock at a time.

![timing-sw-overview](timing-sw-overview.svg)

### Best Master Clock Algorithm
PTP defined the Best Master Clock Algorithm or BMCA. BMCA works in similar ways as the Rapid spanning tree algorithm. It creates a tree rooted at the grandmaster and cuts loops. So there is a single path to reach the grand master from leaf nodes in a given network. Instead of being blocked, like in RSTP, redundant ports are just LISTENING. If the network topology changes, previously LISTENING ports are transitioned to SLAVE state and can be used to retrieve clock timing information.

### Port states
The BMCA defines several port states including:
- MASTER: the port act as a leader clock
- SLAVE: the port acts as a follower clock
- PASSIVE: neither a leader or follower clock
- LISTENING: usually a transition state when deciding state used ti listen to announce messages
- FAULTY: Local state indicating the interface is not operating properly
- DISABLED: no ptp activity

![alt text](port-states.svg)

### Clock states
Locked indicates that the clock is accurately following the external source. The quality of the clock is acceptable.
Holdover indicates that the clock has an acceptable quality even if there is no update from the external clock source (GNSS or GM). The time is maintained by the PHC. The holdover timeout after a configurable amount of time depending on the quality of the PHC. When the holdover times out, the clock goes back to freerun where the clock quality is deemed unacceptable. 
Freerun indicates that the external clock source has been down for too long and the clock quality is now unacceptable. If the clock was looked before, it will eventually start drifting.

![alt text](clock-states.svg)




### Interface aliases
Within the ptp cloud events, the PHC present in the NIC is named after the linux name for the NIC. For instance, a PHC in a nic when ports are names ens1f0, ens1f1, etc, will be name ens1fx.  

## Ordinary clock configuration
In this configuration, we want to synchronize a single server's clock to a grandmaster. ptp4l synchronizes the PHC in the NIC card with updates from the Grandmaster. 
phc2sys synchronizes the system clock with the PHC. 

![oc](oc.svg)

## Boundary clock configuration
In this configuration, we want to both :
- synchronize the server's system clock to a grandmaster
- propagate timing information downstream from the grandmaster to other servers. 
ptp4l synchronizes the PHC in the NIC card with updates from the Grandmaster. 
phc2sys synchronizes the system clock with the PHC. 
An additional port act as a MASTER port to propagate timing information.

![bc](bc.svg)

## Dual NIC Boundary clock configuration
In this configuration, we want to :
- synchronize the server's system clock to a grandmaster
- propagate timing information downstream from the grandmaster to other servers.
- use more than 1 full card to serve more downstream servers
Note: this configuration is not Highly available, only one card PHC clock is synchronizing with the system clock.
ptp4l synchronizes the PHC in the NIC card with updates from the Grandmaster on both cards independently. 
phc2sys synchronizes the system clock with the PHC only from one PHC in one NIC. If this card is lost, the clock from the node will go in HOLDOVER, even if the second card receives PTP updates. 
Additional ports act as MASTER ports to propagate timing information.

![dual-nic-bc](dual-nic-bc.svg)

## Telecom Grandmaster configuration
In this configuration, we define a grand master:
- the reference time is acquired via a GNSS module.
- the NIC card connected to the GNSS is the primary card
- ts2phc synchronizes the GNSS timestamps with the PHC of each (2) NICs in the system.
- phc2sys synchronizes the PHC of the primary NIC to the system clock
- the GNSS timing is directly updating the DPLL of the primary card and transmits 1pps signal to the secondary card
- the 1pps signal avialable in both cards allow transmitting accurate (syncE) HW phase and frequency signals via Ethernets ports.
- The DPLL recovers and propagates the phase and frequency signals from the 1pps port to Ethernet pods and the 1pps out port.

![tgm](tgm.svg)  

## Single NIC Dual Follower configuration
In this configuration, ptp4l operates using the "slaveOnly yes" parameter and uses 2 interfaces to update a PHC.

![dual-follower-overview](dual-follower-overview.svg)

ptp4l selects the best grandmaster based on the clock quality abd priority parameters when both interfaces receive valid ptp updates. If at least one of the interfaces is in "SLAVE" state and locked, the reported ptp-status/lock-state event is LOCKED. If Both interfaces are faulty of not locked, the reported ptp-status/lock-state event is HOLDOVER, then FREERUN once the holdover timeout expires.

![dual-follower](dual-follower.svg)

Same as with OC configuration, when ptp4l no longer receives valid updates from a Grandmaster the PHC clock transitions to HOLDOVER state for the holdover duration. The realtime clock instead goes right to FREERUN.

### Reference ptpconfig

```yaml
apiVersion: ptp.openshift.io/v1
kind: PtpConfig
metadata:
  name: ordinary-clock-1
  namespace: openshift-ptp
spec:
  profile:
  - name: oc1
    phc2sysOpts: -a -r -n 24 -N 8 -R 16 -u 0
    ptp4lConf: |-
      [ens3f2]
      masterOnly 0
      [ens3f3]
      masterOnly 0

      [global]
      #
      # Default Data Set
      #
      slaveOnly 1
      twoStepFlag 1
      priority1 128
      priority2 128
      domainNumber 24
      #utc_offset 37
      clockClass 255
      clockAccuracy 0xFE
      offsetScaledLogVariance 0xFFFF
      free_running 0
      freq_est_interval 1
      dscp_event 0
      dscp_general 0
      dataset_comparison G.8275.x
      G.8275.defaultDS.localPriority 128
      #
      # Port Data Set
      #
      logAnnounceInterval -3
      logSyncInterval -4
      logMinDelayReqInterval -4
      logMinPdelayReqInterval -4
      announceReceiptTimeout 3
      syncReceiptTimeout 0
      delayAsymmetry 0
      fault_reset_interval -4
      neighborPropDelayThresh 20000000
      masterOnly 0
      G.8275.portDS.localPriority 128
      #
      # Run time options
      #
      assume_two_step 0
      logging_level 6
      path_trace_enabled 0
      follow_up_info 0
      hybrid_e2e 0
      inhibit_multicast_service 0
      net_sync_monitor 0
      tc_spanning_tree 0
      tx_timestamp_timeout 50
      unicast_listen 0
      unicast_master_table 0
      unicast_req_duration 3600
      use_syslog 1
      verbose 1
      summary_interval -4
      kernel_leap 1
      check_fup_sync 0
      clock_class_threshold 7
      #
      # Servo Options
      #
      pi_proportional_const 0.0
      pi_integral_const 0.0
      pi_proportional_scale 0.0
      pi_proportional_exponent -0.3
      pi_proportional_norm_max 0.7
      pi_integral_scale 0.0
      pi_integral_exponent 0.4
      pi_integral_norm_max 0.3
      step_threshold 2.0
      first_step_threshold 0.00002
      max_frequency 900000000
      clock_servo pi
      sanity_freq_limit 200000000
      ntpshm_segment 0
      #
      # Transport options
      #
      transportSpecific 0x0
      ptp_dst_mac 01:1B:19:00:00:00
      p2p_dst_mac 01:80:C2:00:00:0E
      udp_ttl 1
      udp6_scope 0x0E
      uds_address /var/run/ptp4l
      #
      # Default interface options
      #
      clock_type OC
      network_transport L2
      delay_mechanism E2E
      time_stamping hardware
      tsproc_mode filter
      delay_filter moving_median
      delay_filter_length 10
      egressLatency 0
      ingressLatency 0
      boundary_clock_jbod 0
      #
      # Clock description
      #
      productDescription ;;
      revisionData ;;
      manufacturerIdentity 00:00:00
      userDescription ;
      timeSource 0xA0
    ptp4lOpts: -2  --summary_interval -4
    ptpClockThreshold:
      holdOverTimeout: 65
      maxOffsetThreshold: 100
      minOffsetThreshold: -100
    ptpSchedulingPolicy: SCHED_FIFO
    ptpSchedulingPriority: 10
  recommend:
  - match:
    - nodeLabel: node-role.kubernetes.io/worker
    priority: 4
    profile: oc1
```