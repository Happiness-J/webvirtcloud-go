package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	ldap "github.com/go-ldap/ldap/v3"
)

// LDAPConfig configures optional directory authentication. When Enabled is
// false, local database authentication remains the only login path.
type LDAPConfig struct {
	Enabled      bool
	URL          string
	BindDN       string
	BindPassword string
	BaseDN       string
	UserFilter   string
	StartTLS     bool
	InsecureTLS  bool
	Timeout      time.Duration
}

// LDAPIdentity is the directory user matched by a successful bind.
type LDAPIdentity struct {
	DN       string
	Username string
}

// LDAPAuthenticator verifies a username and password against an LDAP directory.
type LDAPAuthenticator struct {
	cfg  LDAPConfig
	dial func(url string, tlsConfig *tls.Config) (ldapConn, error)
}

type ldapConn interface {
	StartTLS(config *tls.Config) error
	Bind(username, password string) error
	Search(request *ldap.SearchRequest) (*ldap.SearchResult, error)
	Close() error
}

// NewLDAPAuthenticator validates configuration and returns nil when LDAP is disabled.
func NewLDAPAuthenticator(cfg LDAPConfig) (*LDAPAuthenticator, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.BaseDN) == "" || strings.TrimSpace(cfg.UserFilter) == "" {
		return nil, fmt.Errorf("LDAP URL、Base DN 和用户过滤器都不能为空")
	}
	if !strings.Contains(cfg.UserFilter, "%s") {
		return nil, fmt.Errorf("LDAP 用户过滤器必须包含一个 %%s 用户名占位符")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &LDAPAuthenticator{cfg: cfg, dial: dialLDAP}, nil
}

// Authenticate searches for the username with the service bind, then verifies
// the submitted password by binding as the matched user.
func (a *LDAPAuthenticator) Authenticate(ctx context.Context, username, password string) (LDAPIdentity, error) {
	if a == nil || !a.cfg.Enabled {
		return LDAPIdentity{}, fmt.Errorf("LDAP 未启用")
	}
	if err := ctx.Err(); err != nil {
		return LDAPIdentity{}, err
	}
	if !validLDAPUsername(username) || password == "" {
		return LDAPIdentity{}, fmt.Errorf("LDAP 登录失败")
	}
	conn, err := a.dial(a.cfg.URL, a.tlsConfig())
	if err != nil {
		return LDAPIdentity{}, fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	defer conn.Close()
	if a.cfg.StartTLS {
		if err := conn.StartTLS(a.tlsConfig()); err != nil {
			return LDAPIdentity{}, fmt.Errorf("LDAP StartTLS 失败: %w", err)
		}
	}
	if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
		return LDAPIdentity{}, fmt.Errorf("LDAP 服务账号绑定失败")
	}
	filter := fmt.Sprintf(a.cfg.UserFilter, ldap.EscapeFilter(username))
	result, err := conn.Search(&ldap.SearchRequest{
		BaseDN:     a.cfg.BaseDN,
		Scope:      ldap.ScopeWholeSubtree,
		Filter:     filter,
		Attributes: []string{"uid", "sAMAccountName", "dn"},
		SizeLimit:  2,
		TimeLimit:  int(a.cfg.Timeout.Seconds()),
	})
	if err != nil || len(result.Entries) != 1 {
		return LDAPIdentity{}, fmt.Errorf("LDAP 登录失败")
	}
	entry := result.Entries[0]
	if err := conn.Bind(entry.DN, password); err != nil {
		return LDAPIdentity{}, fmt.Errorf("LDAP 登录失败")
	}
	return LDAPIdentity{DN: entry.DN, Username: username}, nil
}

func (a *LDAPAuthenticator) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: a.cfg.InsecureTLS} // #nosec G402 -- insecure TLS is an explicit operator opt-in for private lab directories.
}

func dialLDAP(url string, tlsConfig *tls.Config) (ldapConn, error) {
	return ldap.DialURL(url, ldap.DialWithTLSConfig(tlsConfig))
}

func validLDAPUsername(username string) bool {
	if len(username) < 1 || len(username) > 64 {
		return false
	}
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
