package netease

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
)

// The weapi scheme: AES-128-CBC twice with two different keys, then the second
// key is encrypted with RSA *without* padding. Everything here is stdlib
// crypto — no third party dependency is needed and nothing extra stays
// resident after the call.

var (
	weapiIV        = []byte("0102030405060708")
	weapiPresetKey = []byte("0CoJUm6Qyw8W8jud")
	weapiPubPEM    = []byte("-----BEGIN PUBLIC KEY-----\n" +
		"MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDgtQn2JZ34ZC28NWYpAUd98iZ3" +
		"7BUrX/aKzmFbt7clFSs6sXqHauqKWqdtLkF2KexO40H1YTX8z2lSgBBOAxLsvak" +
		"lV8k4cBFK9snQXE9/DDaFt6Rr7iVZMldczhC0JNgTz+SHXT6CBHuX3e9SdB1Ua4" +
		"4oncaTWz7OBGLbCiK45wIDAQAB\n-----END PUBLIC KEY-----")
)

var weapiPubKey *rsa.PublicKey

func loadPubKey() (*rsa.PublicKey, error) {
	if weapiPubKey != nil {
		return weapiPubKey, nil
	}
	block, _ := pem.Decode(weapiPubPEM)
	if block == nil {
		return nil, fmt.Errorf("netease: bad public key")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pk, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("netease: not an rsa key")
	}
	weapiPubKey = pk
	return pk, nil
}

func aesCBCEncryptPKCS7(data, key, iv []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	bs := block.BlockSize()
	pad := bs - len(data)%bs
	buf := make([]byte, len(data)+pad)
	copy(buf, data)
	for i := len(data); i < len(buf); i++ {
		buf[i] = byte(pad)
	}
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(buf, buf)
	return buf
}

func randomKey16() []byte {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 16)
	rnd := make([]byte, 16)
	if _, err := rand.Read(rnd); err != nil {
		// Extremely unlikely; fall back to a fixed key so login still works.
		copy(rnd, []byte("0123456789abcdef"))
	}
	for i := 0; i < 16; i++ {
		b[i] = chars[int(rnd[i])%len(chars)]
	}
	return b
}

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}

// rsaNoPadding mirrors NetEase's server side expectation: c = m^e mod n with
// the message left zero padded to the modulus size and the ciphertext hex
// encoded at full length.
func rsaNoPadding(msg []byte, pub *rsa.PublicKey) []byte {
	k := (pub.N.BitLen() + 7) / 8
	buf := make([]byte, k)
	if len(msg) > k {
		copy(buf, msg[len(msg)-k:])
	} else {
		copy(buf[k-len(msg):], msg)
	}
	m := new(big.Int).SetBytes(buf)
	c := new(big.Int).Exp(m, big.NewInt(int64(pub.E)), pub.N)
	out := make([]byte, k)
	cb := c.Bytes()
	copy(out[k-len(cb):], cb)
	return out
}

// Weapi encodes a payload into the params / encSecKey pair.
func Weapi(data map[string]any) (map[string]string, error) {
	text, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	pub, err := loadPubKey()
	if err != nil {
		return nil, err
	}
	secret := randomKey16()
	first := base64.StdEncoding.EncodeToString(aesCBCEncryptPKCS7(text, weapiPresetKey, weapiIV))
	second := base64.StdEncoding.EncodeToString(aesCBCEncryptPKCS7([]byte(first), reverse(secret), weapiIV))
	return map[string]string{
		"params":    second,
		"encSecKey": hex.EncodeToString(rsaNoPadding(secret, pub)),
	}, nil
}

// Eapi is kept for endpoints that only accept the eapi envelope. Not used by
// the default code path, but free to have.
func Eapi(path string, data map[string]any) (map[string]string, error) {
	text, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	digest := md5Hex("nobody" + path + "use" + string(text) + "md5forencrypt")
	plain := path + "-36cd479b6b5-" + string(text) + "-36cd479b6b5-" + digest
	block, err := aes.NewCipher([]byte("e82ckenh8dichen8"))
	if err != nil {
		return nil, err
	}
	bs := block.BlockSize()
	pad := bs - len(plain)%bs
	buf := make([]byte, len(plain)+pad)
	copy(buf, plain)
	for i := len(plain); i < len(buf); i++ {
		buf[i] = byte(pad)
	}
	for i := 0; i < len(buf); i += bs {
		block.Encrypt(buf[i:i+bs], buf[i:i+bs])
	}
	return map[string]string{"params": hexUpper(buf)}, nil
}

func hexUpper(b []byte) string {
	const up = "0123456789ABCDEF"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = up[v>>4]
		out[i*2+1] = up[v&0x0f]
	}
	return string(out)
}
