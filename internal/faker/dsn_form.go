package faker

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func parsePgDSNForm(dsn string) pgDSNForm {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return pgDSNForm{}
	}
	if parsed, ok := parsePgURLDSN(dsn); ok {
		return parsed
	}
	return parsePgKeyValueDSN(dsn)
}

func parsePgURLDSN(dsn string) (pgDSNForm, bool) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return pgDSNForm{}, false
	}
	if u.Scheme != pgSchemePostgres && u.Scheme != pgSchemePostgreSQL {
		return pgDSNForm{}, false
	}

	query := u.Query()
	form := pgDSNForm{
		Host:     strings.TrimSpace(u.Hostname()),
		Port:     strings.TrimSpace(u.Port()),
		Database: strings.TrimSpace(strings.TrimPrefix(u.Path, "/")),
		SSLMode:  strings.TrimSpace(query.Get("sslmode")),
	}
	query.Del("sslmode")
	if len(query) > 0 {
		form.Options = query
	}
	if u.User != nil {
		form.Username = u.User.Username()
		if password, ok := u.User.Password(); ok {
			form.Password = password
		}
	}
	return form, true
}

func parsePgKeyValueDSN(dsn string) pgDSNForm {
	form := pgDSNForm{}
	for key, value := range parsePgKeyValueSettings(dsn) {
		switch key {
		case "host", "hostaddr":
			form.Host = value
		case "port":
			form.Port = value
		case "dbname", "database":
			form.Database = value
		case "user":
			form.Username = value
		case "password", "pass":
			form.Password = value
		case "sslmode":
			form.SSLMode = value
		default:
			if form.Options == nil {
				form.Options = make(url.Values)
			}
			form.Options.Set(key, value)
		}
	}
	return form
}

func buildPgDSN(form pgDSNForm) string {
	host := strings.TrimSpace(form.Host)
	if host == "" {
		host = pgDefaultHost
	}
	port := strings.TrimSpace(form.Port)
	if port == "" {
		port = "5432"
	}
	database := strings.TrimSpace(form.Database)
	username := strings.TrimSpace(form.Username)
	password := form.Password
	sslmode := strings.TrimSpace(form.SSLMode)
	if sslmode == "" {
		sslmode = pgDefaultSSLMode
	}

	u := &url.URL{
		Scheme: pgSchemePostgres,
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + database,
	}
	if username != "" || password != "" {
		u.User = url.UserPassword(username, password)
	}
	q := make(url.Values, len(form.Options)+1)
	for key, values := range form.Options {
		q[key] = append([]string(nil), values...)
	}
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
}

func parsePgKeyValueSettings(dsn string) map[string]string {
	settings := make(map[string]string)
	for position := 0; position < len(dsn); {
		for position < len(dsn) && isDSNSpace(dsn[position]) {
			position++
		}
		keyStart := position
		for position < len(dsn) && dsn[position] != '=' && !isDSNSpace(dsn[position]) {
			position++
		}
		key := strings.ToLower(strings.TrimSpace(dsn[keyStart:position]))
		for position < len(dsn) && isDSNSpace(dsn[position]) {
			position++
		}
		if key == "" || position >= len(dsn) || dsn[position] != '=' {
			for position < len(dsn) && !isDSNSpace(dsn[position]) {
				position++
			}
			continue
		}
		position++
		for position < len(dsn) && isDSNSpace(dsn[position]) {
			position++
		}

		var value strings.Builder
		quoted := position < len(dsn) && dsn[position] == '\''
		if quoted {
			position++
		}
		for position < len(dsn) {
			character := dsn[position]
			if character == '\\' && position+1 < len(dsn) {
				position++
				value.WriteByte(dsn[position])
				position++
				continue
			}
			if quoted && character == '\'' {
				position++
				break
			}
			if !quoted && isDSNSpace(character) {
				break
			}
			value.WriteByte(character)
			position++
		}
		settings[key] = value.String()
	}
	return settings
}

func isDSNSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

func pgDSNCacheKey(dsn string) string {
	form := parsePgDSNForm(dsn)
	host := strings.TrimSpace(form.Host)
	if host == "" {
		host = pgDefaultHost
	}
	db := strings.TrimSpace(form.Database)
	if db == "" {
		return ""
	}
	port := strings.TrimSpace(form.Port)
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("%s/%s", net.JoinHostPort(strings.ToLower(host), port), db)
}

func pgDSNCacheKeys(dsn string) []string {
	primary := pgDSNCacheKey(dsn)
	if primary == "" {
		return nil
	}
	form := parsePgDSNForm(dsn)
	legacyHost := strings.TrimSpace(form.Host)
	if legacyHost == "" {
		legacyHost = pgDefaultHost
	}
	legacy := fmt.Sprintf("%s/%s", legacyHost, strings.TrimSpace(form.Database))
	if legacy == primary {
		return []string{primary}
	}
	return []string{primary, legacy}
}
