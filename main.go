package main

import (
	"encoding/base64"
	"flag"
	"io/ioutil"
	"math/rand"
	"os"

	rtsp "github.com/deepch/sample_rtsp"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"
	log "github.com/sirupsen/logrus"
	"gopkg.in/ini.v1"
)

// 视频宽高
var (
	VideoWidth  int
	VideoHeight int
)

var videoTrackWebRTC *webrtc.Track

func main() {
	var sd, sdInFile, sdOutFile, rtspURL, stunURL, stunUserName, stunPassWorld string
	flag.StringVar(&sd, "sd", "", "sd 参数传入(优先读取)")
	flag.StringVar(&sdInFile, "sdInFile", "./request_sdp.txt", "文件读取sd值(参数未传入, 通过文件读取)")
	flag.StringVar(&sdOutFile, "sdOutFile", "response_sdp.txt", "文件输出sd值")
	flag.StringVar(&stunURL, "stunURL", "turn:cc.zycoo.com:3478?transport=udp", "stun/turn 地址")
	flag.StringVar(&stunUserName, "stunUserName", "tqcenglish", "用户名")
	flag.StringVar(&stunPassWorld, "stunPassWorld", "abcd123?", "密码")
	flag.StringVar(&rtspURL, "rtspURL", "rtsp://admin:Ab123456@192.168.11.65", "rtsp  地址")
	flag.Parse()

	log.SetLevel(log.InfoLevel)

	if sd == "" {
		// 读取文件
		sdBytes, err := ioutil.ReadFile(sdInFile)
		if err != nil {
			log.Error("缺少 sd")
			return
		}
		sd = string(sdBytes)
	}

	log.Infof("stunURL %s, user:%s password:%s\n", stunURL, stunUserName, stunPassWorld)
	resSd := getSd(sd, stunURL, stunUserName, stunPassWorld)
	setSd(sdOutFile, resSd)

	log.Infof("rtspURL %s:\n", rtspURL)
	work(rtspURL)
}

func work(rtspURL string) {
	sps := []byte{}
	pps := []byte{}
	fuBuffer := []byte{}
	count := 0
	Client := rtsp.RtspClientNew()
	Client.Debug = false
	syncCount := 0
	preTS := 0
	writeNALU := func(sync bool, ts int, payload []byte) {
		// if DataChanelTest != nil && preTS != 0 {
		// 	DataChanelTest <- webrtc.RTCSample{Data: payload, Samples: uint32(ts - preTS)}
		// }
		if videoTrackWebRTC != nil && preTS != 0 {
			videoTrackWebRTC.WriteSample(media.Sample{Data: payload, Samples: uint32(ts - preTS)})
		}
		preTS = ts
	}
	handleNALU := func(nalType byte, payload []byte, ts int64) {
		if nalType == 7 {
			if len(sps) == 0 {
				sps = payload
			}
			//	writeNALU(true, int(ts), payload)
		} else if nalType == 8 {
			if len(pps) == 0 {
				pps = payload
			}
			//	writeNALU(true, int(ts), payload)
		} else if nalType == 5 {
			syncCount++
			lastkeys := append([]byte("\000\000\001"+string(sps)+"\000\000\001"+string(pps)+"\000\000\001"), payload...)

			writeNALU(true, int(ts), lastkeys)
		} else {
			if syncCount > 0 {
				writeNALU(false, int(ts), payload)
			}
		}
	}

	if err := Client.Open(rtspURL); err != nil {
		log.Error("[RTSP] Error", err)
	} else {
		for {
			select {
			case <-Client.Signals:
				log.Error("Exit signals by rtsp")
				return
			case data := <-Client.Outgoing:
				count += len(data)

				log.Error("recive  rtp packet size", len(data), "recive all packet size", count)
				if data[0] == 36 && data[1] == 0 {
					cc := data[4] & 0xF
					rtphdr := 12 + cc*4
					ts := (int64(data[8]) << 24) + (int64(data[9]) << 16) + (int64(data[10]) << 8) + (int64(data[11]))
					packno := (int64(data[6]) << 8) + int64(data[7])
					if false {
						log.Println("packet num", packno)
					}
					nalType := data[4+rtphdr] & 0x1F
					if nalType >= 1 && nalType <= 23 {
						if nalType == 6 {
							continue
						}
						handleNALU(nalType, data[4+rtphdr:], ts)
					} else if nalType == 28 {
						isStart := data[4+rtphdr+1]&0x80 != 0
						isEnd := data[4+rtphdr+1]&0x40 != 0
						nalType := data[4+rtphdr+1] & 0x1F
						nal := data[4+rtphdr]&0xE0 | data[4+rtphdr+1]&0x1F
						if isStart {
							fuBuffer = []byte{0}
						}
						fuBuffer = append(fuBuffer, data[4+rtphdr+2:]...)
						if isEnd {
							fuBuffer[0] = nal
							handleNALU(nalType, fuBuffer, ts)
						}
					}
				} else if data[0] == 36 && data[1] == 2 {
					//cc := data[4] & 0xF
					//rtphdr := 12 + cc*4
					//payload := data[4+rtphdr+4:]
				}
			}
		}
	}
	Client.Close()
}

func getSd(data, stunURL, user, password string) (resSd string) {
	sd, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		log.Println(err)
		return
	}
	// webrtc.RegisterDefaultCodecs()
	//peerConnection, err := webrtc.New(webrtc.RTCConfiguration{
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs:           []string{stunURL},
				Username:       user,
				Credential:     password,
				CredentialType: webrtc.ICECredentialTypePassword,
			},
		},
	})
	if err != nil {
		panic(err)
	}
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Infof("Connection State has changed %s \n", connectionState.String())
	})
	vp8Track, err := peerConnection.NewTrack(webrtc.DefaultPayloadTypeH264, rand.Uint32(), "video", "pion2")
	if err != nil {
		log.Println(err)
		return
	}
	_, err = peerConnection.AddTrack(vp8Track)
	if err != nil {
		log.Println(err)
		return
	}
	// fmt.Print(string(sd))
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(sd),
	}
	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		log.Println(err)
		return
	}
	answer, err := peerConnection.CreateAnswer(nil)

	log.Debugf("answer sdp \n %+v", answer.SDP)

	if err != nil {
		log.Println(err)
		return
	}
	// 写回文件
	// DataChanelTest = vp8Track.Samples
	videoTrackWebRTC = vp8Track
	return base64.StdEncoding.EncodeToString([]byte(answer.SDP))
}

func setSd(path, content string) {
	cfg, err := ini.Load(path)
	if err != nil {
		log.Errorf("Fail to read file: %v", err)
		os.Exit(1)
	}

	// Now, make some changes and save it
	cfg.Section("general").Key("sdp").SetValue(content)
	cfg.SaveTo(path)
	log.Infof("写入 resSd 到文件 %s 成功", path)
}
