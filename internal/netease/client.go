// Package netease is a minimal, dependency free client for the (unofficial)
// NetEase Cloud Music HTTP API.
//
// Only the endpoints this player needs are implemented, and every response is
// decoded straight from the network stream into small structs that keep just
// the leaf fields the UI renders. Nothing keeps the raw JSON around: a comment
// page response is ~50 KB on the wire but ~3 KB in memory.
package netease

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	baseURL    = "https://music.163.com"
	userAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	appVersion = "2.10.6"
)

// Track is the compact song model used by lists and the player.
type Track struct {
	ID       int64
	Name     string
	Artists  string // "A / B"
	Album    string
	AlbumID  int64
	Duration int // seconds
	PicURL   string
}

// Playlist is the compact playlist model.
type Playlist struct {
	ID         int64
	Name       string
	Count      int
	Cover      string
	SpecialFor int // 5 == 我喜欢的音乐
	Creator    string
}

// Profile is the logged in user.
type Profile struct {
	UserID   int64
	Nickname string
	Avatar   string
	VIP      bool
}

// Comment is one评论, already flattened to plain text.
type Comment struct {
	ID       int64
	User     string
	Content  string
	Liked    int
	TimeSec  int64
	Replied  string // 被回复者昵称, 可空
	Hot      bool
}

// CommentPage is a single page of comments.
type CommentPage struct {
	Hot      []Comment
	List     []Comment
	Total    int
	More     bool
	MoreHot  bool
}

// Client talks to the API. It is safe for concurrent use; the cookie map is
// guarded because several goroutines may load data at once.
type Client struct {
	HTTP *http.Client

	mu      sync.RWMutex
	cookies map[string]string

	// OnCookiesChanged is called when the cookie jar changes, so the caller
	// can persist the login session.
	OnCookiesChanged func(cookies map[string]string)
}

func NewClient(hc *http.Client) *Client {
	return &Client{HTTP: hc, cookies: map[string]string{}}
}

// flexBool decodes a JSON field that the API sends as a bool in some responses
// and as a number in others.
//
// This is not hypothetical: /api/nuser/account/get returns
//
//	"vipType": 11
//
// and declaring the field as bool made the whole profile decode fail, which the
// app then reported as "login failed" even though the QR login had succeeded
// (state 803, cookies set). One mistyped field must never be able to break
// login again, so every boolean-ish field of an untrusted response goes through
// this type.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	switch s {
	case "", "null", "false", "0", "0.0":
		*b = false
	default:
		*b = true
	}
	return nil
}

// flexInt decodes a JSON integer that may arrive as a quoted string or float.
type flexInt int64

func (n *flexInt) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		*n = flexInt(i)
		return nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		*n = flexInt(int64(f))
		return nil
	}
	*n = 0
	return nil
}

// LoginError carries the NetEase status code so the UI can give an actionable
// hint instead of a raw number.
type LoginError struct {
	Code int
	Msg  string
}

func (e *LoginError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("netease: 登录失败 (code %d)", e.Code)
}

// Well known NetEase login codes.
const (
	CodeNoSuchUser  = 501
	CodeBadPassword = 502
	CodeNeedCaptcha = 503
	CodeRiskControl = 8821
	CodeEncRequired = 401
)

// ---------------------------------------------------------------------------
// cookies
// ---------------------------------------------------------------------------

func (c *Client) SetCookies(m map[string]string) {
	c.mu.Lock()
	c.cookies = map[string]string{}
	for k, v := range m {
		c.cookies[k] = v
	}
	c.mu.Unlock()
}

func (c *Client) Cookies() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		out[k] = v
	}
	return out
}

func (c *Client) cookieHeader() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.cookies) == 0 {
		return "os=pc; appver=" + appVersion + "; osver=10; channel=netease;"
	}
	var b strings.Builder
	for k, v := range c.cookies {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteString("; ")
	}
	return b.String()
}

func (c *Client) absorbCookies(resp *http.Response) {
	sc := resp.Cookies()
	if len(sc) == 0 {
		return
	}
	c.mu.Lock()
	changed := false
	for _, ck := range sc {
		if ck.Value == "" {
			delete(c.cookies, ck.Name)
			changed = true
			continue
		}
		if c.cookies[ck.Name] != ck.Value {
			c.cookies[ck.Name] = ck.Value
			changed = true
		}
	}
	snapshot := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		snapshot[k] = v
	}
	c.mu.Unlock()
	if changed && c.OnCookiesChanged != nil {
		c.OnCookiesChanged(snapshot)
	}
}

// ---------------------------------------------------------------------------
// transport
// ---------------------------------------------------------------------------

type apiError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (c *Client) newRequest(method, u string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("Origin", "https://music.163.com")
	// NetEase's own clients send the client identity as headers too; some
	// endpoints (login) inspect them and reject requests without.
	req.Header.Set("os", "pc")
	req.Header.Set("appver", appVersion)
	req.Header.Set("Cookie", c.cookieHeader())
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req, nil
}

// Warmup establishes a browser-like session: it asks for the home page so the
// server hands out NMTID / __csrf / _ntes_nuid cookies, and seeds its own
// stable device id when the server does not. Without this, login requests look
// like they come from a brand new client and trip risk control.
//
// The warmup body is discarded, never buffered: it is HTML we do not need.
func (c *Client) Warmup() error {
	c.mu.Lock()
	if c.cookies["_ntes_nuid"] == "" {
		c.cookies["_ntes_nuid"] = randomHex(32)
	}
	if c.cookies["deviceId"] == "" {
		c.cookies["deviceId"] = randomHex(32)
	}
	c.mu.Unlock()

	req, err := c.newRequest(http.MethodGet, baseURL+"/", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	c.absorbCookies(resp)
	return nil
}

func randomHex(n int) string {
	const hexdigits = "0123456789abcdef"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n)
	}
	for i := range b {
		b[i] = hexdigits[int(b[i])%16]
	}
	return string(b)
}

// doPost sends a form body and decodes JSON into out from the stream.
func (c *Client) doPost(path string, form url.Values, out any) error {
	req, err := c.newRequest(http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	return c.doDecode(req, out)
}

// doGet sends a GET and decodes JSON into out.
func (c *Client) doGet(path string, q url.Values, out any) error {
	u := baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := c.newRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return c.doDecode(req, out)
}

func (c *Client) doDecode(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	c.absorbCookies(resp)
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("netease: http %d", resp.StatusCode)
	}
	// Decode straight from the body: no full-response buffering.
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("netease: decode: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// login: QR
// ---------------------------------------------------------------------------

// QRKey creates a login key and returns the URL to encode as a QR code.
func (c *Client) QRKey() (string, string, error) {
	var out struct {
		Code   int    `json:"code"`
		UniKey string `json:"unikey"`
	}
	if err := c.doPost("/api/login/qrcode/unikey?type=1", url.Values{"type": {"1"}}, &out); err != nil {
		return "", "", err
	}
	if out.Code != 200 || out.UniKey == "" {
		return "", "", fmt.Errorf("netease: qr key rejected (code %d)", out.Code)
	}
	return out.UniKey, "https://music.163.com/login?codekey=" + out.UniKey, nil
}

// QRState is the result of polling a QR login key.
type QRState int

const (
	QRExpired QRState = 800
	QRWaiting QRState = 801
	QRScanned QRState = 802
	QRSuccess QRState = 803
	QRError   QRState = -1
)

// QRCheck polls the login state of a key.
func (c *Client) QRCheck(key string) (QRState, error) {
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	err := c.doPost("/api/login/qrcode/client/login", url.Values{"key": {key}, "type": {"1"}}, &out)
	if err != nil {
		return QRError, err
	}
	switch out.Code {
	case 800, 801, 802, 803:
		return QRState(out.Code), nil
	case 200:
		return QRSuccess, nil
	default:
		c.expireSession()
		return QRError, fmt.Errorf("netease: qr check %d %s", out.Code, out.Message)
	}
}

// ---------------------------------------------------------------------------
// login: cellphone
// ---------------------------------------------------------------------------

// LoginCellphone logs in with a phone number and password.
//
// Notes learned the hard way (see README): sending an *empty* csrf_token makes
// the server answer 502 "请切换登录方式或升级版本" and never reaches the
// credential check, so the token is only included when the warmup actually
// produced one.
func (c *Client) LoginCellphone(phone, password, countryCode string) (*Profile, error) {
	if countryCode == "" {
		countryCode = "86"
	}
	sum := md5.Sum([]byte(password))
	data := map[string]any{
		"phone":         phone,
		"countrycode":   countryCode,
		"password":      hex.EncodeToString(sum[:]),
		"rememberLogin": "true",
		"type":          "1",
		"https":         "true",
		"remember":      "true",
	}
	return c.login(data)
}

// SendCaptcha asks NetEase to text a 6 digit code to the phone. This is the
// supported way past risk control (code 8821) for password logins.
func (c *Client) SendCaptcha(phone, countryCode string) error {
	if countryCode == "" {
		countryCode = "86"
	}
	params, err := Weapi(map[string]any{"cellphone": phone, "ctcode": countryCode})
	if err != nil {
		return err
	}
	var out apiError
	if err := c.doPost("/weapi/sms/captcha/sent", url.Values{"params": {params["params"]}, "encSecKey": {params["encSecKey"]}}, &out); err != nil {
		return err
	}
	if out.Code != 200 {
		return &LoginError{Code: out.Code, Msg: out.Msg}
	}
	return nil
}

// LoginCaptcha logs in with the SMS code instead of a password.
func (c *Client) LoginCaptcha(phone, captcha, countryCode string) (*Profile, error) {
	if countryCode == "" {
		countryCode = "86"
	}
	data := map[string]any{
		"phone":         phone,
		"countrycode":   countryCode,
		"captcha":       captcha,
		"rememberLogin": "true",
		"type":          "1",
		"https":         "true",
		"remember":      "true",
	}
	return c.login(data)
}

// login performs the shared weapi/login/cellphone call and parses the profile.
func (c *Client) login(data map[string]any) (*Profile, error) {
	if tok := c.csrfToken(); tok != "" {
		data["csrf_token"] = tok
	}
	// The client identity cookies must be present for the server to accept a
	// login from a third party client at all.
	if err := c.Warmup(); err != nil {
		// A failed warmup is not fatal; the login may still work.
		_ = err
	}
	params, err := Weapi(data)
	if err != nil {
		return nil, err
	}
	var out struct {
		apiError
		Profile *struct {
			UserID   int64  `json:"userId"`
			Nickname string `json:"nickname"`
			Avatar   string `json:"avatarUrl"`
			VIP      flexBool `json:"vipType"`
		} `json:"profile"`
		Account *struct {
			ID int64 `json:"id"`
		} `json:"account"`
	}
	if err := c.doPost("/weapi/login/cellphone", url.Values{"params": {params["params"]}, "encSecKey": {params["encSecKey"]}}, &out); err != nil {
		return nil, err
	}
	if out.Profile == nil {
		msg := out.Msg
		if msg == "" {
			msg = fmt.Sprintf("登录失败 (code %d)", out.Code)
		}
		return nil, &LoginError{Code: out.Code, Msg: msg}
	}
	p := &Profile{
		UserID:   out.Profile.UserID,
		Nickname: out.Profile.Nickname,
		Avatar:   out.Profile.Avatar,
		VIP:      bool(out.Profile.VIP),
	}
	c.cacheProfile(p)
	return p, nil
}

func (c *Client) csrfToken() string {
	for k, v := range c.Cookies() {
		if k == "__csrf" {
			return v
		}
	}
	return ""
}

var (
	profMu      sync.RWMutex
	cachedProfile *Profile
)

func (c *Client) cacheProfile(p *Profile) {
	profMu.Lock()
	cachedProfile = p
	profMu.Unlock()
}

func (c *Client) CachedProfile() *Profile {
	profMu.RLock()
	defer profMu.RUnlock()
	return cachedProfile
}

// expireSession drops MUSIC_U so the UI can flip back to the login screen.
func (c *Client) expireSession() {
	c.mu.Lock()
	delete(c.cookies, "MUSIC_U")
	snapshot := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		snapshot[k] = v
	}
	c.mu.Unlock()
	if c.OnCookiesChanged != nil {
		c.OnCookiesChanged(snapshot)
	}
}

// ---------------------------------------------------------------------------
// account / playlists
// ---------------------------------------------------------------------------

// Account returns the current profile, or nil when not logged in.
func (c *Client) Account() (*Profile, error) {
	var out struct {
		apiError
		Profile *struct {
			UserID   int64  `json:"userId"`
			Nickname string `json:"nickname"`
			Avatar   string `json:"avatarUrl"`
			VIP      flexBool `json:"vipType"`
		} `json:"profile"`
	}
	if err := c.doPost("/api/nuser/account/get", url.Values{}, &out); err != nil {
		return nil, err
	}
	if out.Profile == nil || out.Profile.UserID == 0 {
		return nil, nil
	}
	p := &Profile{UserID: out.Profile.UserID, Nickname: out.Profile.Nickname, Avatar: out.Profile.Avatar, VIP: bool(out.Profile.VIP)}
	c.cacheProfile(p)
	return p, nil
}

// UserPlaylists returns 我喜欢的音乐 first, then the user's own playlists.
func (c *Client) UserPlaylists(uid int64) ([]Playlist, error) {
	var out struct {
		apiError
		More     bool `json:"more"`
		Playlist []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			TrackCount int    `json:"trackCount"`
			CoverImg   string `json:"coverImgUrl"`
			SpecialFor int    `json:"specialType"`
			Creator    struct {
				Nickname string `json:"nickname"`
			} `json:"creator"`
		} `json:"playlist"`
	}
	q := url.Values{
		"uid":    {strconv.FormatInt(uid, 10)},
		"limit":  {"1000"},
		"offset": {"0"},
	}
	if err := c.doGet("/api/user/playlist", q, &out); err != nil {
		return nil, err
	}
	list := make([]Playlist, 0, len(out.Playlist))
	// 我喜欢的音乐 first — that is what people open most often.
	for _, p := range out.Playlist {
		if p.SpecialFor == 5 {
			list = append(list, Playlist{ID: p.ID, Name: "我喜欢的音乐", Count: p.TrackCount, Cover: p.CoverImg, SpecialFor: 5, Creator: p.Creator.Nickname})
		}
	}
	for _, p := range out.Playlist {
		if p.SpecialFor == 5 {
			continue
		}
		list = append(list, Playlist{ID: p.ID, Name: p.Name, Count: p.TrackCount, Cover: p.CoverImg, Creator: p.Creator.Nickname})
	}
	return list, nil
}

// PlaylistTracks loads a playlist's songs. Full detail is requested in one go;
// the API caps at 1000 tracks which is fine for this player.
func (c *Client) PlaylistTracks(id int64) (string, []Track, error) {
	var out struct {
		apiError
		Playlist struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			TrackCount int    `json:"trackCount"`
			Tracks     []json.RawMessage `json:"tracks"`
			TrackIDs   []struct {
				ID int64 `json:"id"`
			} `json:"trackIds"`
		} `json:"playlist"`
	}
	form := url.Values{
		"id": {strconv.FormatInt(id, 10)},
		"n":  {"1000"},
		"s":  {"8"},
	}
	if err := c.doPost("/api/v6/playlist/detail", form, &out); err != nil {
		return "", nil, err
	}
	name := out.Playlist.Name
	tracks := make([]Track, 0, len(out.Playlist.TrackIDs))
	seen := make(map[int64]bool, len(out.Playlist.TrackIDs))
	for _, raw := range out.Playlist.Tracks {
		t, ok := parseTrack(raw)
		if ok {
			tracks = append(tracks, t)
			seen[t.ID] = true
		}
	}
	// Newer playlists return trackIds only; fetch the rest in batches.
	var missing []int64
	for _, tid := range out.Playlist.TrackIDs {
		if !seen[tid.ID] {
			missing = append(missing, tid.ID)
		}
	}
	if len(missing) > 0 {
		extra, err := c.songDetails(missing)
		if err == nil {
			tracks = append(tracks, extra...)
		}
	}
	return name, tracks, nil
}

// trackJSON tolerates both song shapes the API is known to return: the modern
// one (al / ar / dt, used by playlist detail) and the legacy one used by
// /api/search/get (album object / artists / duration).
type trackJSON struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Dt       int      `json:"dt"`
	Duration int      `json:"duration"`
	Al       *albumJS `json:"al"`
	Album    *albumJS `json:"album"`
	Ar       []artistJS `json:"ar"`
	Artists  []artistJS `json:"artists"`
}

type albumJS struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

type artistJS struct {
	Name string `json:"name"`
}

func parseTrack(raw json.RawMessage) (Track, bool) {
	var s trackJSON
	if err := json.Unmarshal(raw, &s); err != nil {
		return Track{}, false
	}
	t := Track{ID: s.ID, Name: s.Name, Duration: s.Dt / 1000}
	if t.Duration == 0 && s.Duration > 0 {
		t.Duration = s.Duration / 1000
	}
	alb := s.Al
	if alb == nil {
		alb = s.Album
	}
	if alb != nil {
		t.Album = alb.Name
		t.AlbumID = alb.ID
		t.PicURL = alb.PicURL
	}
	ars := s.Ar
	if len(ars) == 0 {
		ars = s.Artists
	}
	if len(ars) > 0 {
		var b strings.Builder
		for i, a := range ars {
			if i > 0 {
				b.WriteString(" / ")
			}
			b.WriteString(a.Name)
		}
		t.Artists = b.String()
	}
	return t, t.ID != 0
}

func (c *Client) songDetails(ids []int64) ([]Track, error) {
	const batch = 200
	out := make([]Track, 0, len(ids))
	for i := 0; i < len(ids); i += batch {
		j := i + batch
		if j > len(ids) {
			j = len(ids)
		}
		var c2 struct {
			Songs []json.RawMessage `json:"songs"`
		}
		form := url.Values{"c": {buildSongC(ids[i:j])}}
		if err := c.doPost("/api/v3/song/detail", form, &c2); err != nil {
			return out, err
		}
		for _, raw := range c2.Songs {
			if t, ok := parseTrack(raw); ok {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func buildSongC(ids []int64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"id":`)
		b.WriteString(strconv.FormatInt(id, 10))
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String()
}

// ---------------------------------------------------------------------------
// song url / lyric / search
// ---------------------------------------------------------------------------

// SongURL resolves a playable URL. br is in bits per second.
func (c *Client) SongURL(id int64, br int) (string, int, error) {
	var out struct {
		Data []struct {
			URL  *string `json:"url"`
			Br   int     `json:"br"`
			Code int     `json:"code"`
		} `json:"data"`
	}
	form := url.Values{
		"ids": {"[" + strconv.FormatInt(id, 10) + "]"},
		"br":  {strconv.Itoa(br)},
	}
	if err := c.doPost("/api/song/enhance/player/url", form, &out); err != nil {
		return "", 0, err
	}
	if len(out.Data) == 0 || out.Data[0].URL == nil || *out.Data[0].URL == "" {
		return "", 0, ErrNoURL
	}
	return *out.Data[0].URL, out.Data[0].Br, nil
}

var ErrNoURL = errors.New("netease: 该歌曲无版权或需要 VIP")

// Lyric returns the original and (optional) translated LRC text.
func (c *Client) Lyric(id int64) (string, string, error) {
	var out struct {
		apiError
		Lrc struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		Tlyric struct {
			Lyric string `json:"lyric"`
		} `json:"tlyric"`
	}
	q := url.Values{"id": {strconv.FormatInt(id, 10)}, "lv": {"-1"}, "tv": {"-1"}}
	if err := c.doGet("/api/song/lyric", q, &out); err != nil {
		return "", "", err
	}
	return out.Lrc.Lyric, out.Tlyric.Lyric, nil
}

// Search finds songs by keyword. The legacy search endpoint omits cover URLs,
// so the hits are enriched with one batched song-detail call.
func (c *Client) Search(keyword string, limit int) ([]Track, error) {
	if limit <= 0 {
		limit = 30
	}
	var out struct {
		apiError
		Result struct {
			Songs []json.RawMessage `json:"songs"`
		} `json:"result"`
	}
	form := url.Values{
		"s":      {keyword},
		"type":   {"1"},
		"limit":  {strconv.Itoa(limit)},
		"offset": {"0"},
	}
	if err := c.doPost("/api/search/get", form, &out); err != nil {
		return nil, err
	}
	tracks := make([]Track, 0, len(out.Result.Songs))
	ids := make([]int64, 0, len(out.Result.Songs))
	for _, raw := range out.Result.Songs {
		if t, ok := parseTrack(raw); ok {
			tracks = append(tracks, t)
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return tracks, nil
	}
	// Enrich: cover art + exact duration + album name.
	details, err := c.songDetails(ids)
	if err != nil {
		return tracks, nil // search results are still usable
	}
	byID := make(map[int64]Track, len(details))
	for _, d := range details {
		byID[d.ID] = d
	}
	for i := range tracks {
		if d, ok := byID[tracks[i].ID]; ok {
			if d.PicURL != "" {
				tracks[i].PicURL = d.PicURL
			}
			if d.Album != "" {
				tracks[i].Album = d.Album
			}
			if d.Duration > 0 {
				tracks[i].Duration = d.Duration
			}
			if d.Artists != "" {
				tracks[i].Artists = d.Artists
			}
		}
	}
	return tracks, nil
}

// ---------------------------------------------------------------------------
// comments
// ---------------------------------------------------------------------------

// Comments loads one page. offset is the paging cursor for the "newest" list;
// hot comments only come back on the first page (offset == 0).
func (c *Client) Comments(songID int64, offset, limit int) (*CommentPage, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var out struct {
		apiError
		Total     int               `json:"total"`
		More      bool              `json:"more"`
		MoreHot   bool              `json:"moreHot"`
		HotList   []json.RawMessage `json:"hotComments"`
		CommentL  []json.RawMessage `json:"comments"`
	}
	form := url.Values{
		"rid":        {strconv.FormatInt(songID, 10)},
		"limit":      {strconv.Itoa(limit)},
		"offset":     {strconv.Itoa(offset)},
		"beforeTime": {"0"},
	}
	if err := c.doPost("/api/v1/resource/comments/R_SO_4_"+strconv.FormatInt(songID, 10), form, &out); err != nil {
		return nil, err
	}
	page := &CommentPage{Total: out.Total, More: out.More, MoreHot: out.MoreHot}
	if offset == 0 {
		for _, raw := range out.HotList {
			if cm, ok := parseComment(raw); ok {
				cm.Hot = true
				page.Hot = append(page.Hot, cm)
			}
		}
	}
	for _, raw := range out.CommentL {
		if cm, ok := parseComment(raw); ok {
			page.List = append(page.List, cm)
		}
	}
	return page, nil
}

func parseComment(raw json.RawMessage) (Comment, bool) {
	var s struct {
		CommentID int64 `json:"commentId"`
		Content   string `json:"content"`
		Liked     int   `json:"likedCount"`
		Time      int64 `json:"time"`
		User      struct {
			Nickname string `json:"nickname"`
		} `json:"user"`
		BeReplied []struct {
			User struct {
				Nickname string `json:"nickname"`
			} `json:"user"`
		} `json:"beReplied"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return Comment{}, false
	}
	cm := Comment{
		ID:      s.CommentID,
		User:    s.User.Nickname,
		Content: flatten(s.Content),
		Liked:   s.Liked,
		TimeSec: s.Time / 1000,
	}
	if len(s.BeReplied) > 0 {
		cm.Replied = s.BeReplied[0].User.Nickname
	}
	if cm.User == "" {
		cm.User = "网易云用户"
	}
	return cm, cm.ID != 0
}

// flatten strips the emoji/img/@ markup the API embeds in lyrics and comments
// and keeps human readable text, per the "degrade rich text" requirement.
func flatten(s string) string {
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s, "[]") {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '[' {
			if j := strings.IndexByte(s[i:], ']'); j > 0 {
				tag := s[i+1 : i+j]
				if strings.HasPrefix(tag, "em") || strings.HasPrefix(tag, "img") {
					i += j
					continue
				}
				if strings.HasPrefix(tag, "@") || tag == "" {
					i += j
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return strings.TrimSpace(b.String())
}

// DebugWeapi sends an arbitrary weapi request and returns the raw body and any
// cookies set, so login problems can be diagnosed without a debugger. It is
// only used by nmprobe.
func (c *Client) DebugWeapi(path string, data map[string]any) (int, []byte, []string, error) {
	params, err := Weapi(data)
	if err != nil {
		return 0, nil, nil, err
	}
	form := url.Values{"params": {params["params"]}, "encSecKey": {params["encSecKey"]}}
	req, err := c.newRequest(http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var cks []string
	for _, ck := range resp.Cookies() {
		cks = append(cks, ck.Name+"="+ck.Value)
	}
	c.absorbCookies(resp)
	return resp.StatusCode, body, cks, nil
}

// DebugPost sends a plain /api/ POST and returns the raw body.
func (c *Client) DebugPost(path string, form url.Values) (int, []byte, []string, error) {
	req, err := c.newRequest(http.MethodPost, baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var cks []string
	for _, ck := range resp.Cookies() {
		cks = append(cks, ck.Name+"="+ck.Value)
	}
	c.absorbCookies(resp)
	return resp.StatusCode, body, cks, nil
}

// PicURL returns a sized variant of a NetEase image URL.
func PicURL(raw string, size int) string {
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "?param=") {
		return raw + "?param=" + strconv.Itoa(size) + "y" + strconv.Itoa(size)
	}
	return raw
}

// Now returns unix seconds; kept here so callers share one clock helper.
func Now() int64 { return time.Now().Unix() }
