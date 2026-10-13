package mqttservice

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Mosquitto 2.x password-file format: `<username>:$7$<iterations>$<b64 salt>$<b64 hash>`
// where $7$ is PBKDF2-SHA512. Verified against mosquitto 2.1.2
// (`mosquitto_passwd -b` output, and an end-to-end CONNECT against the real
// broker — see scripts/mqtt-acl-verify.sh).
const (
	mosquittoHashScheme  = "$7$"
	mosquittoIterations  = 1000
	mosquittoSaltBytes   = 64
	mosquittoDerivedSize = 64
	// BrokerSecretBytes is the raw entropy behind a broker password. 24 bytes
	// of crypto/rand entropy, base64url-encoded into a 32-char secret.
	BrokerSecretBytes = 24
)

// ErrInvalidBrokerUsername is returned for a username that cannot be used with
// a broker ACL. `+` and `#` are MQTT wildcards: a username containing either
// would make `pattern ... /%u/gps` match topics it should not
// (mosquitto#1610, docs/13 §4.2).
var ErrInvalidBrokerUsername = errors.New("invalid broker username")

// ValidateBrokerUsername rejects empty usernames and any containing an MQTT
// wildcard or whitespace, which would silently widen an ACL pattern.
func ValidateBrokerUsername(username string) error {
	if username == "" {
		return fmt.Errorf("%w: empty", ErrInvalidBrokerUsername)
	}
	if strings.ContainsAny(username, "+# \t\r\n") {
		return fmt.Errorf("%w: %q contains a wildcard or whitespace", ErrInvalidBrokerUsername, username)
	}
	return nil
}

// NewBrokerSecret mints a broker password. It returns the plaintext (handed to
// the device exactly once — it is never stored) plus the mosquitto password
// hash to append to the broker's password file.
func NewBrokerSecret() (secret, hash string, err error) {
	raw := make([]byte, BrokerSecretBytes)
	if _, err = rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate broker secret: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(raw)
	salt := make([]byte, mosquittoSaltBytes)
	if _, err = rand.Read(salt); err != nil {
		return "", "", fmt.Errorf("generate broker salt: %w", err)
	}
	hash = hashBrokerSecret(secret, salt)
	return secret, hash, nil
}

// hashBrokerSecret renders the mosquitto password-hash field for a secret.
func hashBrokerSecret(secret string, salt []byte) string {
	derived, err := pbkdf2.Key(sha512.New, secret, salt, mosquittoIterations, mosquittoDerivedSize)
	if err != nil {
		// Only an absurd keyLength can fail; treat it as a hard error anyway.
		panic("pbkdf2: " + err.Error())
	}
	return fmt.Sprintf("%s%d$%s$%s",
		mosquittoHashScheme,
		mosquittoIterations,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(derived),
	)
}

// BrokerPasswordFileLine renders one `<username>:<hash>` line for the broker's
// password_file. Operators append these with `mosquitto_passwd -b` semantics;
// the file never holds a plaintext password.
func BrokerPasswordFileLine(username, hash string) string {
	return username + ":" + hash
}
