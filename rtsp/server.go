package rtsp

import (
	"encoding/base64"
	"math/rand"
	"os"

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

// StunConfig stun 信息
type StunConfig struct {
	URL      string
	UserName string
	PassWord string
}

var videoTrackWebRTC *webrtc.Track

//StartRTSPServer 开启 RTSP 服务
func StartRTSPServer(rtspURL string, sdpOutFile string, remoteSdp string, stun *StunConfig) {

	localSdp := getSdp(remoteSdp, stun)
	setSdp(sdpOutFile, localSdp)

	log.Infof("rtspURL %s:\n", rtspURL)
	work(rtspURL)
}

func work(rtspURL string) {
	sps := []byte{}
	pps := []byte{}
	fuBuffer := []byte{}
	count := 0

	client := ClientNew()
	client.URL = rtspURL
	client.Debug = false
	client.Name = rtspURL

	syncCount := 0
	preTS := 0
	writeNALU := func(sync bool, ts int, payload []byte) {
		// if DataChanelTest != nil && preTS != 0 {
		// 	DataChanelTest <- webrtc.RTCSample{Data: payload, Samples: uint32(ts - preTS)}
		// }
		if videoTrackWebRTC != nil && preTS != 0 {
			log.Debug("videoTrackWebRTC")
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

	if err := client.Open(); err != nil {
		log.Error("[RTSP] Error", err)
	} else {
		for {
			select {
			case <-client.Signals:
				log.Error("Exit signals by rtsp")
				return
			case data := <-client.Outgoing:
				count += len(data)

				// log.Error("recive  rtp packet size", len(data), "recive all packet size", count)
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
	client.Close()
}

func getSdp(remoteSdp string, stun *StunConfig) (local string) {
	sd, err := base64.StdEncoding.DecodeString(remoteSdp)
	if err != nil {
		log.Println(err)
		return
	}
	// webrtc.RegisterDefaultCodecs()
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs:           []string{stun.URL},
				Username:       stun.UserName,
				Credential:     stun.PassWord,
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

func setSdp(path, content string) {
	cfg, err := ini.Load(path)
	if err != nil {
		log.Errorf("Fail to read file: %v", err)
		os.Exit(1)
	}

	// Now, make some changes and save it
	cfg.Section("general").Key("sdp").SetValue(content)
	cfg.SaveTo(path)
	log.Infof("写入 remoteSdp 到文件 %s 成功", path)
}
