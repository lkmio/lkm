package rtsp

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)
import "math/rand"

func generateNonce() string {
	k := make([]byte, 12)
	for bytes := 0; bytes < len(k); {
		n, err := rand.Read(k[bytes:])
		if err != nil {
			panic("rand.Read() failed")
		}
		bytes += n
	}
	return base64.StdEncoding.EncodeToString(k)
}

func generateAuthHeader(realm string) string {
	return fmt.Sprintf(`Digest realm="%s", nonce="%s" algorithm=MD5`,
		realm, generateNonce())
}

func h(data string) string {
	hash := md5.New()
	hash.Write([]byte(data))
	return hex.EncodeToString(hash.Sum(nil))
}

func calculateResponse(username, realm, nonce, uri, password string) string {
	//H(data) = MD5(data)
	//KD(secret, data) = H(concat(secret, ":", data))
	//request-digest  = <"> < KD ( H(A1), unq(nonce-value) ":" H(A2) ) > <">
	A1 := fmt.Sprintf("%s:%s:%s", username, realm, password)
	A2 := fmt.Sprintf("%s:%s", "DESCRIBE", uri)

	return h(h(A1) + ":" + nonce + ":" + h(A2))
}

func parseAuthParams(value string) (map[string]string, error) {
	index := strings.Index(value, "Digest ")
	if index == -1 {
		return nil, fmt.Errorf("unknow scheme %s", value)
	}

	pairs := strings.Split(value[len("Digest "):], ",")
	m := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		i := strings.Index(pair, "=")
		if i < 0 {
			m[pair] = ""
		} else if i == len(pair)-1 {
			m[pair[:i]] = ""
		} else {
			m[strings.TrimSpace(pair[:i])] = strings.Trim(pair[i+1:], "\"")
		}
	}

	return m, nil
}

func DoAuthenticatePlainTextPassword(params map[string]string, password string) bool {
	response := calculateResponse(params["username"], params["realm"], params["nonce"], params["uri"], password)
	return response == params["response"]
}
