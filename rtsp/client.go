package rtsp

import (
	sdp "RTSPtoWebRTC/rtsp/sdp"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"html"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pion/webrtc/v2"
	log "github.com/sirupsen/logrus"
)

//Client 属性
type Client struct {
	URL           string          // rtsp 地址
	Debug         bool            // 是否开启调试
	VideoTracks   []*webrtc.Track // webrtc 对端
	Name          string
	rtspTimeOut   time.Duration
	rtptimeout    time.Duration
	keepalivetime int
	cseq          int
	uri           string
	host          string
	port          string
	login         string
	password      string
	session       string
	bauth         string
	nonce         string
	realm         string
	sdp           string
	track         []string
	socket        net.Conn
	firstvideots  int
	firstaudiots  int
	Signals       chan bool
	Outgoing      chan []byte
}

func init() {
	log.SetReportCaller(true)
}

// ClientNew 新建客户端
func ClientNew() *Client {
	return &Client{
		cseq:          1,
		rtspTimeOut:   3,
		rtptimeout:    10,
		keepalivetime: 20,
		Signals:       make(chan bool, 1),
		Outgoing:      make(chan []byte, 100000)}
}

//Open 打开 rtsp 连接
func (client *Client) Open() (err error) {
	if err := client.ParseURL(client.URL); err != nil {
		return err
	}
	if err := client.Connect(); err != nil {
		return err
	}
	if err := client.Write("OPTIONS", "", "", false, false); err != nil {
		return err
	}
	if err := client.Write("DESCRIBE", "", "", false, false); err != nil {
		return err
	}
	i := 0
	p := 1
	for _, track := range client.track {
		if err := client.Write("SETUP", "/"+track, "Transport: RTP/AVP/TCP;unicast;interleaved="+strconv.Itoa(i)+"-"+strconv.Itoa(p)+"\r\n", false, false); err != nil {
			return err
		}
		i++
		p++
	}
	if err := client.Write("PLAY", "", "", false, false); err != nil {
		return err
	}
	go client.RtspRtpLoop()
	return
}

//Connect tcp 连接
func (client *Client) Connect() (err error) {
	var socket net.Conn
	option := &net.Dialer{Timeout: client.rtspTimeOut * time.Second}
	if socket, err = option.Dial("tcp", client.host+":"+client.port); err != nil {
		log.Error(err)
		return err
	}
	client.socket = socket
	return
}

// Write write
func (client *Client) Write(method string, track, add string, stage bool, noread bool) (err error) {
	client.cseq++
	status := 0
	if err := client.socket.SetDeadline(time.Now().Add(client.rtspTimeOut * time.Second)); err != nil {
		return err
	}
	message := method + " " + client.uri + track + " RTSP/1.0\r\nCSeq: " + strconv.Itoa(client.cseq) + "\r\n" + add + client.session + client.Dauth(method) + client.bauth + "User-Agent: Lavf57.8.102\r\n\r\n"
	if client.Debug {
		log.Println(message)
	}
	if _, err := client.socket.Write([]byte(message)); err != nil {
		return err
	}
	if noread {
		return
	}
	if responce, err := client.Read(); err != nil {
		return err
	} else {
		fLines := strings.SplitN(string(responce), " ", 3)
		if status, err = strconv.Atoi(fLines[1]); err != nil {
			return err
		}
		if status == 401 && !stage {
			client.bauth = "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(client.login+":"+client.password)) + "\r\n"
			client.nonce = ParseDirective(string(responce), "nonce")
			client.realm = ParseDirective(string(responce), "realm")
			if err := client.Write(method, "", "", true, false); err != nil {
				return err
			}
		} else if status == 401 {
			return errors.New("Method " + method + " Authorization failed")
		} else if status != 200 {
			return errors.New("Method " + method + " Return bad status code")
		} else {
			switch method {
			case "SETUP":
				client.ParseSetup(string(responce))
			case "DESCRIBE":
				client.ParseDescribe(string(responce))
			case "PLAY":
				client.ParsePlay(string(responce))
			}
		}
	}
	return
}

//Read read
func (client *Client) Read() (buffer []byte, err error) {
	buffer = make([]byte, 4096)
	if err = client.socket.SetDeadline(time.Now().Add(client.rtspTimeOut * time.Second)); err != nil {
		log.Error(err)
		return nil, err
	}

	var n int
	if n, err = client.socket.Read(buffer); err != nil || n <= 2 {
		return nil, err
	}
	if client.Debug {
		log.Println(string(buffer[:n]))
	}
	return buffer[:n], nil
}

//ParseURL parse urls
func (client *Client) ParseURL(uri string) (err error) {
	elemets, err := url.Parse(html.UnescapeString(uri))
	if err != nil {
		return err
	}
	if host, port, err := net.SplitHostPort(elemets.Host); err == nil {
		client.host = host
		client.port = port
	} else {
		client.host = elemets.Host
		client.port = "554"
	}
	if elemets.User != nil {
		client.login = elemets.User.Username()
		client.password, _ = elemets.User.Password()
	}
	if elemets.RawQuery != "" {
		client.uri = "rtsp://" + client.host + ":" + client.port + elemets.Path + "?" + elemets.RawQuery
	} else {
		client.uri = "rtsp://" + client.host + ":" + client.port + elemets.Path
	}
	return
}

//Dauth auth
func (client *Client) Dauth(phase string) string {
	dauth := ""
	if client.nonce != "" {
		hs1 := client.GetMD5Hash(client.login + ":" + client.realm + ":" + client.password)
		hs2 := client.GetMD5Hash(phase + ":" + client.uri)
		responce := client.GetMD5Hash(hs1 + ":" + client.nonce + ":" + hs2)
		dauth = `Authorization: Digest username="` + client.login + `", realm="` + client.realm + `", nonce="` + client.nonce + `", uri="` + client.uri + `", response="` + responce + `"` + "\r\n"
	}
	return dauth
}

//ParseDirective directive
func ParseDirective(message, name string) string {
	index := strings.Index(message, name)
	if index == -1 {
		return ""
	}
	start := 1 + index + strings.Index(message[index:], `"`)
	end := start + strings.Index(message[start:], `"`)
	return strings.TrimSpace(message[start:end])
}

//GetMD5Hash md5
func (client *Client) GetMD5Hash(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

// ParseSetup setup
func (client *Client) ParseSetup(message string) {
	mparsed := strings.Split(message, "\r\n")
	for _, element := range mparsed {
		if strings.Contains(element, "Session:") {
			if strings.Contains(element, ";") {
				fist := strings.Split(element, ";")[0]
				client.session = "Session: " + fist[9:] + "\r\n"
			} else {
				client.session = "Session: " + element[9:] + "\r\n"
			}
		}
	}
}

// ParseDescribe desc
func (client *Client) ParseDescribe(message string) {
	sdpstring := strings.Split(message, "\r\n\r\n")
	if len(sdpstring) > 1 {
		client.sdp = sdpstring[1]
		for _, info := range sdp.Decode(sdpstring[1]) {
			client.track = append(client.track, info.Control)
		}
	} else {
		if client.Debug {
			log.Println("SDP not found")
		}
	}
}

// ParsePlay play
func (client *Client) ParsePlay(message string) {
	fist := true
	mparsed := strings.Split(message, "\r\n")
	for _, element := range mparsed {
		if strings.Contains(element, "RTP-Info") {
			mparseds := strings.Split(element, ",")
			for _, elements := range mparseds {
				mparsedss := strings.Split(elements, ";")
				if len(mparsedss) > 2 {
					if fist {
						client.firstvideots, _ = strconv.Atoi(mparsedss[2][8:])
						fist = false
					} else {
						client.firstaudiots, _ = strconv.Atoi(mparsedss[2][8:])
					}
				}
			}
		}
	}
}

//RtspRtpLoop loop
func (client *Client) RtspRtpLoop() {
	defer func() {
		client.Signals <- true
	}()
	header := make([]byte, 4)
	payload := make([]byte, 16384)
	sync_b := make([]byte, 1)
	timer := time.Now()
	start_t := true

	for {
		if int(time.Now().Sub(timer).Seconds()) > client.keepalivetime {
			if err := client.Write("OPTIONS", "", "", false, true); err != nil {
				return
			}
			timer = time.Now()
		}
		if start_t {
			client.socket.SetDeadline(time.Now().Add(50 * time.Second))
		} else {
			client.socket.SetDeadline(time.Now().Add(client.rtptimeout * time.Second))
		}
		if n, err := io.ReadFull(client.socket, header); err != nil || n != 4 {
			if client.Debug {
				log.Println("read header error", err)
			}
			return
		}
		if header[0] != 36 {
			rtsp := false
			if string(header) != "RTSP" {
				if client.Debug {
					log.Println("desync strange data repair", string(header), header, client.uri)
				}
			} else {
				rtsp = true
			}
			i := 1
			for {
				i++
				if i > 4096 {
					if client.Debug {
						log.Println("desync fatal miss position rtp packet", client.uri)
					}
					return
				}
				if n, err := io.ReadFull(client.socket, sync_b); err != nil && n != 1 {
					return
				}
				if sync_b[0] == 36 {
					header[0] = sync_b[0]
					if n, err := io.ReadFull(client.socket, sync_b); err != nil && n != 1 {
						return
					}
					if sync_b[0] == 0 || sync_b[0] == 1 || sync_b[0] == 2 || sync_b[0] == 3 {
						header[1] = sync_b[0]
						if n, err := io.ReadFull(client.socket, header[2:]); err != nil && n == 2 {
							return
						}
						if !rtsp {
							if client.Debug {
								log.Println("desync fixed ok", sync_b[0], client.uri, i, "afrer byte")
							}
						}
						break
					} else {
						if client.Debug {
							log.Println("desync repair fail chanel incorect", sync_b[0], client.uri)
						}
					}
				}
			}
		}
		payloadLen := (int)(header[2])<<8 + (int)(header[3])
		if payloadLen > 16384 || payloadLen < 12 {
			if client.Debug {
				log.Println("fatal size desync", client.uri, payloadLen)
			}
			continue
		}
		if n, err := io.ReadFull(client.socket, payload[:payloadLen]); err != nil || n != payloadLen {
			if client.Debug {
				log.Println("read payload error", payloadLen, err)
			}
			return
		} else {
			start_t = false
			client.Outgoing <- append(header, payload[:n]...)
		}
	}
}

// Close 关闭
func (client *Client) Close() {
	if client.socket != nil {
		if err := client.socket.Close(); err != nil {
		}
	}
}

//IsConnect 判断是否连接
func (client *Client) IsConnect() bool {
	return client.socket != nil
}
