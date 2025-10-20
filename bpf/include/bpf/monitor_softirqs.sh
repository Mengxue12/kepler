#!/bin/bash

while true; do
    # get the first sample
    mapfile -t first < <(grep -E 'NET_RX|NET_TX|BLOCK' /proc/softirqs)
    sleep 3
    # get the second sample
    mapfile -t second < <(grep -E 'NET_RX|NET_TX|BLOCK' /proc/softirqs)

    echo "=== SoftIRQ delta in 3 seconds ==="
    for i in {0..2}; do
        # get the row name (NET_RX, NET_TX, BLOCK)
        name=$(echo "${first[$i]}" | awk -F: '{print $1}')
        
        # get the first row CPU values
        first_values=($(echo "${first[$i]}" | awk -F: '{print $2}'))
        second_values=($(echo "${second[$i]}" | awk -F: '{print $2}'))

        echo -n "$name: "
        total_diff=0
        for j in "${!first_values[@]}"; do
            diff=$(( ${second_values[$j]} - ${first_values[$j]} ))
            echo -n "$diff "
            total_diff=$(( total_diff + diff ))
        done
        echo "(total: $total_diff)"
    done
    echo
done
