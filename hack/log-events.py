import json
import re
import sys
from datetime import datetime
from enum import Enum

class ActionType(Enum):
    PUSH = "PUSH"
    PULL = "PULL"

RESET = "\033[0m"
GREEN = "\033[32m"
RED = "\033[31m"
YELLOW = "\033[33m"
CYAN = "\033[36m"
BOLD = "\033[1m"
DIM = "\033[2m"

STATE_COLORS = {
    "LOCKED": GREEN,
    "FREERUN": RED,
    "HOLDOVER": YELLOW,
    "ACQUIRING": YELLOW,
}

def resource_short_name(address):
    """Extract the interface/port part from a full ResourceAddress."""
    parts = address.rstrip("/").split("/")
    # Typically: /cluster/node/<hostname>/<iface>/<role>
    if len(parts) >= 2:
        return "/".join(parts[-2:])
    return address

def extract_iface(source):
    """Extract the network interface from a CloudEvent source path."""
    parts = source.rstrip("/").split("/")
    # /cluster/node/<hostname>/<iface>[/<role>]
    if len(parts) >= 5:
        return parts[4]
    return None

def format_values(values):
    """Extract meaningful fields from the data.values array."""
    state = None
    offset = None
    clock_class = None
    resource = None

    for v in values:
        resource = resource or v.get("ResourceAddress", "")
        if v.get("data_type") == "notification" and v.get("value_type") == "enumeration":
            state = v.get("value")
        elif v.get("data_type") == "metric":
            val = v.get("value")
            if clock_class is None and state is None:
                clock_class = val
            else:
                offset = val

    parts = []
    if resource:
        parts.append(f"{CYAN}{resource_short_name(resource)}{RESET}")
    if state:
        color = STATE_COLORS.get(state, YELLOW)
        parts.append(f"state={color}{BOLD}{state}{RESET}")
    if offset is not None:
        parts.append(f"offset={BOLD}{offset}{RESET}")
    if clock_class is not None:
        parts.append(f"class={BOLD}{clock_class}{RESET}")
    return "  ".join(parts)

def process_log(log_message, push_pull):
    try:
        log_message = log_message.replace('\\n', '\n')
        log_message = log_message.replace('\\"', '"')

        json_part = re.search(r'\{.*\}', log_message, re.DOTALL).group()
        if not json_part:
            print("No JSON part found in log message: " + log_message)
            return

        log_entry = json.loads(json_part)

        type_value = log_entry["type"]
        time_value = log_entry["time"]
        direction_value = ""
        if push_pull == ActionType.PULL:
            direction_value = "⬅️"
        elif push_pull == ActionType.PUSH:
            direction_value = "➡️"

        time_value_fixed = re.sub(r"(\.\d{4})\d+", r"\1", time_value)

        short_type = type_value.replace("event.sync.sync-status.", "").replace("event.sync.ptp-status.", "")
        values_info = ""
        if "data" in log_entry and "values" in log_entry["data"]:
            values_info = format_values(log_entry["data"]["values"])

        iface = extract_iface(log_entry.get("source", ""))
        iface_tag = f" {CYAN}{iface}{RESET}" if iface else ""
        print(f"{direction_value} {time_value_fixed}:{iface_tag} {GREEN}{short_type}{RESET}  {values_info}", flush=True)
    except Exception as e:
        print(f"Error processing log message: {e}", flush=True)

def main():
    print("Listening for events...")
    try:
        for log_message in sys.stdin:
            log_message = log_message.strip()
            if "event sent" in log_message and log_message:
                process_log(log_message, ActionType.PUSH)
            elif "received event" in log_message and log_message:
                process_log(log_message, ActionType.PUSH)
            elif "Got CurrentState" in log_message and log_message:
                process_log(log_message, ActionType.PULL)

    except KeyboardInterrupt:
        print("\nStopping log message processing.")

if __name__ == "__main__":
    main()
