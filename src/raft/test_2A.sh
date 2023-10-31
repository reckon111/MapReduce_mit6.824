#! /bin/bash

for i in {0..100}
do 
    go test -run 2A -race > "logRaftA_${i}.txt"
    if grep -q "FAIL" "logRaftA_${i}.txt"
    then
        echo "logRaftA_${i}.txt"
        exit 1  # 退出脚本
    fi
done

rm logRaftA_*