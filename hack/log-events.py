import json
import re
import sys
from datetime import datetime
from enum import Enum

class ActionType(Enum):
    PUSH = "PUSH"
    PULL = "PULL"

def process_log(log_message, push_pull):
    try:
        # Remove escape characters
        log_message = log_message.replace('\\"', '"')

        # Extract JSON part from the log message
        json_part = re.search(r'{.*}', log_message).group()
        if not json_part:
            print("No JSON part found in log message: " + log_message)
            return

        # Decode the JSON message
        log_entry = json.loads(json_part)

        # ANSI escape code for green color
        green_color = "\033[32m"
        reset_color = "\033[0m"

        # Print the value of "type" in green color
        type_value = log_entry["type"]
        time_value = log_entry["time"]
        direction_value = ""
        if push_pull == ActionType.PULL:
            direction_value = "⬅️"
        elif push_pull == ActionType.PUSH:
            direction_value = "➡️"


        # Format the time value to only 4 digits of precision after the decimal point
        time_value_fixed = re.sub(r"(\.\d{4})\d+", r"\1", time_value)
        print(f"{direction_value} {time_value_fixed}: {green_color}{type_value}{reset_color}")
    except Exception as e:
        print(f"Error processing log message: {e}")

def main():
    print("Listening for events...")
    try:
        for log_message in sys.stdin:
            log_message = log_message.strip()
            if "received event" in log_message and log_message:
                process_log(log_message, ActionType.PUSH)
            if "Got CurrentState" in log_message and log_message:
                process_log(log_message, ActionType.PULL)

    except KeyboardInterrupt:
        print("\nStopping log message processing.")

if __name__ == "__main__":
    main()
