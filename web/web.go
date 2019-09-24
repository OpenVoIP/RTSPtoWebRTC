package main

import "github.com/pion/webrtc/v2"

// 视频宽高
var (
	VideoWidth  int
	VideoHeight int
)

var videoTrackWebRTC *webrtc.Track

func main() {
	go StartHTTPServer()
	select {}
}
