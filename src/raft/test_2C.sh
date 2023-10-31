#! /bin/bash

for i in {0..100}
do 
    go test -run 2C -race > "logRaftC_${i}.txt"
    if grep -q "FAIL" "logRaftC_${i}.txt"
    then
        echo "logRaftC_${i}.txt"
        exit 1  # 退出脚本
    fi
done

rm logRaftC_*