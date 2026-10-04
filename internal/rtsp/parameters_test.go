package rtsp

import "testing"

func TestExtractAndMergeH264ParameterSets(t *testing.T) {
	data:=[]byte{
		0,0,0,1,0x67,1,2,
		0,0,1,0x68,3,4,
		0,0,0,1,0x65,5,
	}
	sets:=ExtractParameterSets("H264",data)
	if len(sets)!=2 { t.Fatalf("sets=%d",len(sets)) }
	merged:=MergeParameterSets("H264",[][]byte{{0x67,9}},sets)
	if len(merged)!=2 { t.Fatalf("merged=%d",len(merged)) }
	if merged[0][1]!=1 { t.Fatalf("new SPS did not replace old: %v",merged[0]) }
}

func TestExtractH265ParameterSets(t *testing.T) {
	data:=[]byte{
		0,0,0,1,32<<1,1,1,
		0,0,0,1,33<<1,1,2,
		0,0,0,1,34<<1,1,3,
		0,0,0,1,19<<1,1,4,
	}
	sets:=ExtractParameterSets("H265",data)
	if len(sets)!=3 { t.Fatalf("sets=%d",len(sets)) }
}
