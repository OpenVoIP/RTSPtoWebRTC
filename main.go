package main

import (
	"RTSPtoWebRTC/rtsp"
	"flag"
	"io/ioutil"

	log "github.com/sirupsen/logrus"
)

// 参数信息
var (
	sdpInFile  string
	sdpOutFile string
	rtspURL    string
)

// main 开始
func main() {
	stun := &rtsp.StunConfig{}

	flag.StringVar(&sdpInFile, "sdpInFile", "./request_sdp.txt", "文件读取sd值(参数未传入, 通过文件读取)")
	flag.StringVar(&sdpOutFile, "sdpOutFile", "response_sdp.txt", "文件输出sd值")

	flag.StringVar(&stun.URL, "stunURL", "turn:cc.zycoo.com:3478", "stun/turn 地址")
	flag.StringVar(&stun.UserName, "stunUserName", "tqcenglish", "用户名")
	flag.StringVar(&stun.PassWord, "stunPassWord", "abcd123", "密码")

	flag.StringVar(&rtspURL, "rtspURL", "rtsp://admin:Ab123456@192.168.11.65", "rtsp  地址")
	flag.Parse()

	// 读取文件
	sdBytes, err := ioutil.ReadFile(sdpInFile)
	if err != nil {
		log.Error("缺少 sdp")
		return
	}
	sdp := string(sdBytes)

	log.Infof("stun inf %+v\n", stun)

	log.SetLevel(log.DebugLevel)
	go rtsp.StartRTSPServer(rtspURL, sdpOutFile, sdp, stun)

	select {}
}
