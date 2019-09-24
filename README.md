# RTSPtoWebRTC

``` shell
go clean --modcache
go build
gox -osarch="linux/arm"
scp RTSPtoWebRTC_linux_arm root@192.168.12.201:/userdata

```

- [webrtc](https://github.com/pions/webrtc)