#!/bin/bash

# Pool URL
pool_url=stratum+tcp://solo.ckpool.org:3333
# Pool username
username=12SqPdxL1byPEZFnQfC1P52aHbZg9yTSMb
# Pool password
password=x
# ASIC frequency
frequency=150
# Suggested difficulty
difficulty=100


# Job timer (0-2000)  * Avoid modifying *
aon_job_timer=20
# Baudrate (1=1M, 2=1.5M)
aon_baudrate=2
# USB Hub for automatic reset (may require root)
# Knup HB505/506    =  35d6:2510
# Ampere V3         =  05e3:0608
aon_usb_hub="35d6:2510"

./smminer -o "$pool_url" -u "$username" -p "$password" --aon-frequency "$frequency" -aon-job-timer "$aon_job_timer" -suggest-diff "$difficulty" -aon-baudrate "$aon_baudrate" -aon-usb-hub "$aon_usb_hub"
