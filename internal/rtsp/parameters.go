package rtsp

import "strings"

func ExtractParameterSets(codec string, annexB []byte) [][]byte {
	codec=strings.ToUpper(codec)
	if codec=="HEVC" { codec="H265" }
	var out [][]byte
	for _,nal:=range splitAnnexBNALs(annexB) {
		if len(nal)==0 { continue }
		switch codec {
		case "H264":
			t:=nal[0]&0x1f
			if t==7 || t==8 { out=append(out,append([]byte(nil),nal...)) }
		case "H265":
			if len(nal)<2 { continue }
			t:=(nal[0]>>1)&0x3f
			if t==32 || t==33 || t==34 { out=append(out,append([]byte(nil),nal...)) }
		}
	}
	return out
}

func MergeParameterSets(codec string, current, discovered [][]byte) [][]byte {
	codec=strings.ToUpper(codec)
	if codec=="HEVC" { codec="H265" }
	byType:=make(map[uint8][]byte)
	put:=func(nal []byte) {
		if len(nal)==0 { return }
		var t uint8
		switch codec {
		case "H264":
			t=nal[0]&0x1f
			if t!=7 && t!=8 { return }
		case "H265":
			if len(nal)<2 { return }
			t=(nal[0]>>1)&0x3f
			if t!=32 && t!=33 && t!=34 { return }
		default:
			return
		}
		byType[t]=append([]byte(nil),nal...)
	}
	for _,nal:=range current { put(nal) }
	for _,nal:=range discovered { put(nal) }

	order:=[]uint8{7,8}
	if codec=="H265" { order=[]uint8{32,33,34} }
	out:=make([][]byte,0,len(order))
	for _,t:=range order {
		if nal:=byType[t]; len(nal)>0 { out=append(out,nal) }
	}
	return out
}

func splitAnnexBNALs(data []byte) [][]byte {
	var starts []struct{ pos, size int }
	for i:=0;i+3<len(data); {
		if i+4<=len(data) && data[i]==0 && data[i+1]==0 && data[i+2]==0 && data[i+3]==1 {
			starts=append(starts,struct{pos,size int}{i,4})
			i+=4
			continue
		}
		if data[i]==0 && data[i+1]==0 && data[i+2]==1 {
			starts=append(starts,struct{pos,size int}{i,3})
			i+=3
			continue
		}
		i++
	}
	if len(starts)==0 { return nil }
	out:=make([][]byte,0,len(starts))
	for i,start:=range starts {
		begin:=start.pos+start.size
		end:=len(data)
		if i+1<len(starts) { end=starts[i+1].pos }
		for end>begin && data[end-1]==0 { end-- }
		if end>begin { out=append(out,data[begin:end]) }
	}
	return out
}
