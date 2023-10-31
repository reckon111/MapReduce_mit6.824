#! /bin/bash

for i in {0..100}
do 
    go test -run 2B -race > "logRaft_${i}.txt"
    if grep -q "FAIL" "logRaft_${i}.txt"
    then
        echo "logRaft_${i}.txt"
        exit 1  # 退出脚本
    fi
done

rm logRaft_*