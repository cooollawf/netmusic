// nmprobe is a tiny debugging CLI for the netease package. It is not part of
// the shipped player, but it makes API and decoder problems reproducible
// without touching the GUI.
package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/go-mp3"

	"netmusic/internal/audio"
	"netmusic/internal/netease"
	"netmusic/internal/netx"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cl := netease.NewClient(netx.NewHTTPClient(netx.ProxyMode("")))
	switch os.Args[1] {
	case "search":
		probeSearch(cl, strings.Join(os.Args[2:], " "))
	case "url":
		probeURL(cl, os.Args[2])
	case "play":
		probePlay(cl, os.Args[2])
	case "bench":
		benchDecode(cl, os.Args[2])
	case "lyric":
		probeLyric(cl, os.Args[2])
	case "comments":
		probeComments(cl, os.Args[2], os.Args[3:]...)
	case "account":
		probeAccount(cl)
	case "qr":
		probeQR(cl)
	case "playlists":
		probePlaylists(cl, os.Args[2])
	case "proxy":
		probeProxy()
	case "raw":
		probeRaw(os.Args[2], os.Args[3:]...)
	case "audio":
		probeAudio(cl, os.Args[2])
	default:
		usage()
	}
}

// probeAudio drives the real WASAPI path for a few seconds so the audio stack
// can be validated without the GUI.
func probeAudio(cl *netease.Client, idStr string) {
	id, _ := strconv.ParseInt(idStr, 10, 64)
	u, br, err := cl.SongURL(id, 128000)
	if err != nil {
		fmt.Println("no url:", err)
		return
	}
	data, err := download(u)
	if err != nil {
		fmt.Println("download:", err)
		return
	}
	fmt.Printf("loaded %.2f MB, br=%d\n", float64(len(data))/1048576, br)
	pl := audio.NewPlayer()
	pl.SetOnError(func(e error) { fmt.Println("player error:", e) })
	pl.SetOnEnd(func() { fmt.Println("player: track ended") })
	pl.Load(data, 240)
	pl.SetVolume(60)
	t0 := time.Now()
	for i := 0; i < 12; i++ {
		time.Sleep(400 * time.Millisecond)
		fmt.Printf("  t=%.1fs pos=%.2fs playing=%v\n", time.Since(t0).Seconds(), pl.Position(), pl.IsPlaying())
	}
	fmt.Println("seek to 50%")
	pl.SeekFrac(0.5)
	time.Sleep(1200 * time.Millisecond)
	fmt.Printf("  pos after seek=%.2fs\n", pl.Position())
	pl.Close()
	fmt.Println("closed")
}

// probeRaw dumps the first bytes of a raw API response, for diagnosing shape
// mismatches without a debugger.
func probeRaw(path string, form ...string) {
	body := strings.Join(form, "&")
	u := "https://music.163.com" + path
	req, _ := http.NewRequest("POST", u, strings.NewReader(body))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("Cookie", "os=pc; appver=2.10.6;")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tr := &http.Transport{Proxy: netx.ProxyFunc()}
	c := &http.Client{Transport: tr, Timeout: 20 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d len=%d\n%s\n", resp.StatusCode, len(b), clip(string(b), 800))
}

func usage() {
	fmt.Fprintln(os.Stderr, `nmprobe commands:
  search <keyword>            歌曲搜索
  url <songID>                取直链（并打印大小/码率）
  play <songID> <seconds>     下载并用 go-mp3 解码 N 秒，报告速度与内存
  bench <songID>              完整解码计时（决定 seek 策略）
  lyric <songID>              歌词
  comments <songID> [offset]  评论
  account                     当前账号
  qr                          二维码登录流程
  playlists <uid>             用户歌单`)
}

func probeProxy() {
	fmt.Println("mode:", netx.ResolveProxyMode(), netx.ProxyURL())
}

func probeSearch(cl *netease.Client, kw string) {
	t0 := time.Now()
	tracks, err := cl.Search(kw, 10)
	fmt.Printf("search %q: %v (%d hits, %v)\n", kw, err, len(tracks), time.Since(t0))
	for _, t := range tracks {
		fmt.Printf("  %d  %-32s %-20s %3ds\n", t.ID, clip(t.Name, 32), clip(t.Artists, 20), t.Duration)
	}
}

func probeURL(cl *netease.Client, idStr string) {
	id, _ := strconv.ParseInt(idStr, 10, 64)
	u, br, err := cl.SongURL(id, 128000)
	fmt.Printf("url: %v br=%d err=%v\n  %s\n", u != "", br, err, u)
	if err != nil {
		return
	}
	n, err := quickSize(u)
	fmt.Printf("content-length=%d err=%v\n", n, err)
}

func probePlay(cl *netease.Client, idStr string) {
	id, _ := strconv.ParseInt(idStr, 10, 64)
	u, _, err := cl.SongURL(id, 128000)
	if err != nil {
		fmt.Println("no url:", err)
		return
	}
	data, err := download(u)
	if err != nil {
		fmt.Println("download:", err)
		return
	}
	fmt.Printf("downloaded %d bytes (%.2f MB)\n", len(data), float64(len(data))/1048576)
	dec, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		fmt.Println("decoder:", err)
		return
	}
	fmt.Printf("sampleRate=%d length=%d bytes (%.1fs)\n", dec.SampleRate(), dec.Length(), float64(dec.Length())/176400)
	t0 := time.Now()
	buf := make([]byte, 17640*4) // 100ms
	var total int
	for {
		n, err := dec.Read(buf)
		total += n
		if err != nil || n == 0 {
			break
		}
	}
	el := time.Since(t0)
	fmt.Printf("decoded %.1f MB in %v => %.1fx realtime\n", float64(total)/1048576, el, (float64(total)/176400)/el.Seconds())
}

func benchDecode(cl *netease.Client, idStr string) {
	id, _ := strconv.ParseInt(idStr, 10, 64)
	u, _, err := cl.SongURL(id, 128000)
	if err != nil {
		fmt.Println("no url:", err)
		return
	}
	data, err := download(u)
	if err != nil {
		fmt.Println("download:", err)
		return
	}
	fmt.Printf("file=%d bytes\n", len(data))
	// whole-file decode
	t0 := time.Now()
	dec, _ := mp3.NewDecoder(bytes.NewReader(data))
	n, _ := io.Copy(io.Discard, dec)
	full := time.Since(t0)
	fmt.Printf("full decode: %d bytes in %v (%.1fx realtime)\n", n, full, (float64(n)/176400)/full.Seconds())

	// seek benchmark: how expensive is a mid-file seek with go-mp3?
	if dec2, err := mp3.NewDecoder(bytes.NewReader(data)); err == nil {
		t1 := time.Now()
		pos, err := dec2.Seek(int64(float64(dec2.Length())*0.9), io.SeekStart)
		fmt.Printf("seek to 90%%: pos=%d err=%v in %v\n", pos, err, time.Since(t1))
	}
}

func probeLyric(cl *netease.Client, idStr string) {
	id, _ := strconv.ParseInt(idStr, 10, 64)
	lrc, tlrc, err := cl.Lyric(id)
	fmt.Printf("lyric err=%v len=%d translated=%d\n", err, len(lrc), len(tlrc))
	lines := strings.Split(lrc, "\n")
	for i, l := range lines {
		if i > 8 {
			break
		}
		fmt.Println("  ", l)
	}
}

func probeComments(cl *netease.Client, idStr string, args ...string) {
	id, _ := strconv.ParseInt(idStr, 10, 64)
	offset := 0
	if len(args) > 0 {
		offset, _ = strconv.Atoi(args[0])
	}
	t0 := time.Now()
	p, err := cl.Comments(id, offset, 20)
	if err != nil {
		fmt.Println("comments:", err)
		return
	}
	fmt.Printf("comments: total=%d more=%v hot=%d list=%d in %v\n", p.Total, p.More, len(p.Hot), len(p.List), time.Since(t0))
	for i, c := range p.Hot {
		if i > 2 {
			break
		}
		fmt.Printf("  [hot] %s (%d赞): %s\n", c.User, c.Liked, clip(c.Content, 70))
	}
	for i, c := range p.List {
		if i > 2 {
			break
		}
		fmt.Printf("  [new] %s (%d赞): %s\n", c.User, c.Liked, clip(c.Content, 70))
	}
}

func probeAccount(cl *netease.Client) {
	p, err := cl.Account()
	fmt.Printf("account: %+v err=%v\n", p, err)
}

func probeQR(cl *netease.Client) {
	key, qrURL, err := cl.QRKey()
	fmt.Printf("qr key=%s err=%v\n  url=%s\n", key, err, qrURL)
	if err != nil {
		return
	}
	for i := 0; i < 3; i++ {
		st, err := cl.QRCheck(key)
		fmt.Printf("  poll %d -> state=%d err=%v\n", i, st, err)
		if err != nil || st == netease.QRExpired {
			return
		}
		time.Sleep(time.Second)
	}
}

func probePlaylists(cl *netease.Client, uidStr string) {
	uid, _ := strconv.ParseInt(uidStr, 10, 64)
	pls, err := cl.UserPlaylists(uid)
	fmt.Printf("playlists: %v (%d)\n", err, len(pls))
	for i, p := range pls {
		if i > 11 {
			break
		}
		fmt.Printf("  %d  %-28s %4d首\n", p.ID, clip(p.Name, 28), p.Count)
	}
	if len(pls) > 0 {
		name, tracks, err := cl.PlaylistTracks(pls[0].ID)
		fmt.Printf("tracks of %q: %v (%d)\n", name, err, len(tracks))
		for i, t := range tracks {
			if i > 5 {
				break
			}
			fmt.Printf("    %d %s - %s\n", t.ID, clip(t.Name, 30), clip(t.Artists, 24))
		}
	}
}

// ---------------------------------------------------------------------------

func httpGet(u string) (*http.Response, error) {
	tr := &http.Transport{
		Proxy: netx.ProxyFunc(),
		DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
	}
	c := &http.Client{Transport: tr, Timeout: 60 * time.Second}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	return c.Do(req)
}

func download(u string) ([]byte, error) {
	resp, err := httpGet(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func quickSize(u string) (int64, error) {
	pu, err := url.Parse(u)
	if err != nil {
		return 0, err
	}
	_ = pu
	resp, err := httpGet(u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.ContentLength, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
