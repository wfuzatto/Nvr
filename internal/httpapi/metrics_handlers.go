package httpapi

import (
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

type systemStatus struct {
	Version string `json:"version"`
	UptimeSeconds int64 `json:"uptime_seconds"`
	Goroutines int `json:"goroutines"`
	MemoryAllocBytes uint64 `json:"memory_alloc_bytes"`
	MemorySysBytes uint64 `json:"memory_sys_bytes"`
	DiskTotalBytes uint64 `json:"disk_total_bytes"`
	DiskFreeBytes uint64 `json:"disk_free_bytes"`
	DiskUsedPercent float64 `json:"disk_used_percent"`
	CamerasTotal int `json:"cameras_total"`
	CamerasRecording int `json:"cameras_recording"`
	CamerasError int `json:"cameras_error"`
	Reconnects uint64 `json:"reconnects"`
	FramesPublished uint64 `json:"frames_published"`
	SegmentsWritten uint64 `json:"segments_written"`
	RecordedBytes uint64 `json:"recorded_bytes"`
	Broker any `json:"broker,omitempty"`
	WebRTC any `json:"webrtc,omitempty"`
}

func (s *Server) collectSystemStatus() systemStatus {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	status:=systemStatus{
		Version:s.deps.Version,
		UptimeSeconds:int64(time.Since(s.startedAt).Seconds()),
		Goroutines:runtime.NumGoroutine(),
		MemoryAllocBytes:mem.Alloc,
		MemorySysBytes:mem.Sys,
		CamerasTotal:len(s.deps.Cameras.List()),
	}
	var stat syscall.Statfs_t
	if syscall.Statfs(s.deps.Config.StorageDir,&stat)==nil {
		status.DiskTotalBytes=stat.Blocks*uint64(stat.Bsize)
		status.DiskFreeBytes=stat.Bavail*uint64(stat.Bsize)
		if status.DiskTotalBytes>0 {
			status.DiskUsedPercent=float64(status.DiskTotalBytes-status.DiskFreeBytes)*100/float64(status.DiskTotalBytes)
		}
	}
	if s.deps.Media!=nil {
		for _,item:=range s.deps.Media.Statuses() {
			switch item.State {
			case "recording": status.CamerasRecording++
			case "error","reconnecting": status.CamerasError++
			}
			status.Reconnects+=item.Reconnects
			status.FramesPublished+=item.FramesPublished
			status.SegmentsWritten+=item.SegmentsWritten
			status.RecordedBytes+=uint64(max64(item.BytesWritten,0))
		}
		status.Broker=s.deps.Media.BrokerStats()
	}
	if s.deps.WebRTC!=nil { status.WebRTC=s.deps.WebRTC.Stats() }
	return status
}

func (s *Server) handleSystemStatus(w http.ResponseWriter,_ *http.Request) {
	writeJSON(w,http.StatusOK,s.collectSystemStatus())
}

func (s *Server) handlePrometheus(w http.ResponseWriter,_ *http.Request) {
	st:=s.collectSystemStatus()
	w.Header().Set("Content-Type","text/plain; version=0.0.4; charset=utf-8")
	metrics:=[]struct{name,help string; value float64}{
		{"nvr_uptime_seconds","NVR process uptime in seconds",float64(st.UptimeSeconds)},
		{"nvr_goroutines","Current Go goroutine count",float64(st.Goroutines)},
		{"nvr_memory_alloc_bytes","Go heap allocated bytes",float64(st.MemoryAllocBytes)},
		{"nvr_memory_sys_bytes","Go runtime system bytes",float64(st.MemorySysBytes)},
		{"nvr_storage_total_bytes","Recording filesystem total bytes",float64(st.DiskTotalBytes)},
		{"nvr_storage_free_bytes","Recording filesystem free bytes",float64(st.DiskFreeBytes)},
		{"nvr_storage_used_percent","Recording filesystem used percentage",st.DiskUsedPercent},
		{"nvr_cameras_total","Configured cameras",float64(st.CamerasTotal)},
		{"nvr_cameras_recording","Cameras currently recording",float64(st.CamerasRecording)},
		{"nvr_cameras_error","Cameras in error or reconnecting state",float64(st.CamerasError)},
		{"nvr_media_reconnects_total","RTSP reconnect attempts",float64(st.Reconnects)},
		{"nvr_media_frames_published_total","Frames published to Frame Broker",float64(st.FramesPublished)},
		{"nvr_media_segments_written_total","Recording segments finalized",float64(st.SegmentsWritten)},
		{"nvr_media_recorded_bytes_total","Video bytes written by active workers",float64(st.RecordedBytes)},
	}
	for _,m:=range metrics {
		fmt.Fprintf(w,"# HELP %s %s\n# TYPE %s gauge\n%s %s\n",m.name,m.help,m.name,m.name,strconv.FormatFloat(m.value,'f',-1,64))
	}
}

func max64(v int64,min int64) int64 { if v<min { return min }; return v }
