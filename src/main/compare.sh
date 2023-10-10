sort mr-out* | grep . > mr-wc-all

rm mr-out*
go run -race mrsequential.go wc.so pg*.txt
sort mr-out-0 > mr-correct-wc.txt

if cmp mr-wc-all mr-correct-wc.txt
then
  echo '---' wc test: PASS
else
  echo '---' wc output is not the same as mr-correct-wc.txt
  echo '---' wc test: FAIL
  failed_any=1
fi