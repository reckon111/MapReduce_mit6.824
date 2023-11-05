#! /bin/bash

for i in {0..10}
do 
    go test -race > "logRaft_${i}.txt"
    if grep -q "FAIL" "logRaft_${i}.txt"
    then
        echo "logRaft_${i}.txt"
        exit 1  # 退出脚本
    fi
done

rm logRaft_*