#! /bin/bash

for i in {0..100}
do 
    go test -run 2B -race > "logRaftB_${i}.txt"
    if grep -q "FAIL" "logRaftB_${i}.txt"
    then
        echo "logRaftB_${i}.txt"
        exit 1  # 退出脚本
    fi
done

rm logRaftB_*