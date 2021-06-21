package event

import (
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	ptp_metrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	log "github.com/sirupsen/logrus"
	"math"
	"strings"
	"sync"
)

// Stats calculates stats  nolint:unused
type Stats struct {
	num                 float64
	max                 float64
	min                 float64
	mean                float64
	sumSqr              float64
	sumDiffSqr          float64
	frequencyAdjustment float64
	delayFromMaster     float64
}

func (s *Stats) addValue(val float64) {

	oldMean := s.mean

	if s.num == 0 || s.max < val {
		s.max = val
	}
	if s.num == 0 && s.min > val {
		s.mean = val
	}
	s.num++
	s.mean = oldMean + (val-oldMean)/s.num
	s.sumSqr += val * val
	s.sumDiffSqr += (val - oldMean) * (val - s.mean)

}

// get stdDev
func (s *Stats) getStdev() float64 { //nolint:unused
	if s.num > 0 {
		return math.Sqrt(s.sumDiffSqr / s.num)
	}
	return 1
}
func (s *Stats) getMaxAbs() float64 {
	if s.max > s.mean {
		return s.max
	}
	return s.min

}

// reset status
func (s *Stats) reset() { //nolint:unused
	s.num = 0
	s.max = 0
	s.mean = 0
	s.sumDiffSqr = 0
	s.sumSqr = 0
}

// Worker define worker object
type Worker struct {
	sync.RWMutex
	jobQueue  chan Job
	quit      chan bool
	stats     map[string]*Stats
	lastState cneevent.SyncState
	wg        *sync.WaitGroup
}

// NewWorker create new worker
func NewWorker(wg *sync.WaitGroup) *Worker {
	return &Worker{
		jobQueue: make(chan Job, 20),
		quit:     make(chan bool),
		wg:       wg,
		stats:    map[string]*Stats{},
		lastState:"",
	}
}

// Stop worker from  processing jobs
func (w *Worker) Stop() {
	w.quit <- true
}

// Start worker to process jobs from the queue
func (w *Worker) Start() {
	defer w.wg.Done()
	w.wg.Add(1)
	go func(w *Worker) {
		defer w.wg.Done()
		for {
			select {
			case job := <-w.jobQueue: // see if anything has been assigned to the queue
				offsetFromMaster, frequencyAdjustment, delayFromMaster, clockState := job.Process(w.lastState)
				if clockState != "" {
					w.lastState = clockState
					if job.processName == phc2sysProcessName {
						if s, found := w.stats[job.iface]; found {
							s.addValue(offsetFromMaster)
							s.frequencyAdjustment = frequencyAdjustment
							s.delayFromMaster = delayFromMaster
						} else { //TODO: lock
							s := Stats{frequencyAdjustment: frequencyAdjustment,
								delayFromMaster: delayFromMaster,
							}
							s.addValue(offsetFromMaster)
							w.stats[job.iface] = &s
						}
						metrics.UpdatePTPMetrics(job.processName, job.iface, offsetFromMaster, w.stats[job.iface].getMaxAbs(), frequencyAdjustment, delayFromMaster)
					}
				} else {
					//reset stats if there is a failure
					if s, found := w.stats[job.iface]; found {
						s.reset()
					}
				}
			case <-w.quit:
				return
			}
		}
	}(w)
}

// Job to process
type Job struct {
	processName string
	iface       string
	time        string
	output      string
}

// NewJob create new job object
func NewJob(processName, iface, time, output string) Job {
	return Job{
		processName: processName,
		iface:       iface,
		time:        time,
		output:      output,
	}
}

// Process jobs that are present in the queue
func (j *Job) Process(lastClockState cneevent.SyncState) (offsetFromMaster, frequencyAdjustment, delayFromMaster float64, clockState cneevent.SyncState) {
	if j.processName == phc2sysProcessName {
		offsetFromMaster, frequencyAdjustment, delayFromMaster, clockState = ExtractPhc2sysEvent(j.processName, j.iface, j.output)
	} else if j.processName == ptp4lProcessName {
		clockState = ExtractPTP4lEvent(j.processName, j.iface, j.output)
	}
	if clockState != "" {
		GenEvent(j.iface, offsetFromMaster, clockState, lastClockState)
	}
	return
}

// Start - starts the dispatcher routine
func (d *Dispatcher) Start() {
	d.wg.Add(1)
	go d.dispatch()
	d.wg.Add(1)
	go d.worker.Start()
}

// Stop - stops the workers and dispatcher routine
func (d *Dispatcher) Stop() {
	d.quit <- true
}

// Dispatcher to dispatch  jobs to worker
type Dispatcher struct {
	sync.RWMutex
	jobQueue  chan Job
	workQueue chan Job
	worker    *Worker // one worker per interface
	quit      chan bool
	wg        *sync.WaitGroup
}

// NewDispatcher creates, and returns a new Dispatcher object.
func NewDispatcher(wg *sync.WaitGroup) *Dispatcher {
	return &Dispatcher{
		jobQueue:  make(chan Job, 20),
		workQueue: make(chan Job, 20),
		worker:    NewWorker(wg),
		quit:      make(chan bool),
		wg:        wg,
	}
}

func (d *Dispatcher) dispatch() {
	defer d.wg.Done()
	for {
		select {
		case job := <-d.jobQueue: // We got something in on our queue
			d.worker.jobQueue <- job
		case <-d.quit:
			d.worker.Stop()
			return
		}
	}
}

// Submit ... submit job to job queue
func (d *Dispatcher) Submit(job Job) {
	d.jobQueue <- job
}

// ProcessMsg log message for events
func (d *Dispatcher) ProcessMsg(msg string) {
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output := replacer.Replace(msg)
	fields := strings.Fields(output)
	if len(fields) > 2 {
		ExtractEvent(fields[0], fields[2], fields[1], msg)
		job := NewJob(fields[0], fields[2], fields[1], msg)
		d.Submit(job) //submit event
		// process metrics
		ptp_metrics.ExtractMetrics(fields[0], fields[2], msg)
	} else {
		log.Info("ignoring log:log  is not in required format processname[time]: [iface]")
	}
}
