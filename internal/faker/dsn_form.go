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
	scheme, rest, ok := strings.Cut(dsn, "://")
	if !ok || (scheme != pgSchemePostgres && scheme != pgSchemePostgreSQL) {
		return pgDSNForm{}, false
	}
	end := strings.IndexAny(rest, "/?")
	if end < 0 {
		end = len(rest)
	}
	authority := rest[:end]
	userinfo := ""
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		userinfo, authority = authority[:at+1], authority[at+1:]
	}
	// net/url does not accept every PostgreSQL multi-host authority (for
	// example host1:5432,host2). Parse the credentials/path separately.
	u, err := url.Parse(strings.ReplaceAll(scheme+"://"+userinfo+"localhost"+rest[end:], "#", "%23"))
	if err != nil || u.Scheme == "" {
		return pgDSNForm{}, false
	}

	// PostgreSQL URIs use percent encoding, not HTML form encoding: a
	// literal '+' is a plus sign, and later duplicate settings take precedence.
	query := make(url.Values)
	sslWasLast := false
	for pair := range strings.SplitSeq(u.RawQuery, "&") {
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return pgDSNForm{}, false
		}
		key, keyErr := url.PathUnescape(key)
		value, valueErr := url.PathUnescape(value)
		if keyErr != nil || valueErr != nil {
			return pgDSNForm{}, false
		}
		if key == "dbname" {
			key = "database"
		}
		if key == "ssl" || key == "sslmode" {
			sslWasLast = key == "ssl"
		}
		query.Set(key, value)
	}
	if query.Get("ssl") == "true" {
		query.Del("ssl")
		if sslWasLast {
			query.Set("sslmode", "require")
		}
	}
	hosts := strings.Split(authority, ",")
	ports := make([]string, len(hosts))
	for i, authority := range hosts {
		host, port := authority, ""
		if strings.HasPrefix(host, "[") {
			closeBracket := strings.IndexByte(host, ']')
			if closeBracket < 0 {
				return pgDSNForm{}, false
			}
			port = strings.TrimPrefix(host[closeBracket+1:], ":")
			host = host[1:closeBracket]
		} else {
			host, port, _ = strings.Cut(host, ":")
		}
		host, err := url.PathUnescape(host)
		if err != nil {
			return pgDSNForm{}, false
		}
		port, err = url.PathUnescape(port)
		if err != nil {
			return pgDSNForm{}, false
		}
		hosts[i], ports[i] = host, port
	}
	form := pgDSNForm{
		Host:     strings.Join(hosts, ","),
		Port:     strings.Join(ports, ","),
		Database: strings.TrimSpace(strings.TrimPrefix(u.Path, "/")),
		SSLMode:  strings.TrimSpace(query.Get("sslmode")),
	}
	if u.User != nil {
		form.Username = u.User.Username()
		if password, ok := u.User.Password(); ok {
			form.Password = password
		}
	}
	// Preserve the legacy hostaddr alias when no host was supplied. pgx does
	// not consume hostaddr and would send it as an invalid server parameter.
	if form.Host == "" && query.Get("host") == "" {
		form.Host = query.Get("hostaddr")
	}
	query.Del("hostaddr")
	for key, target := range map[string]*string{
		"host": &form.Host, "port": &form.Port, "database": &form.Database,
		"user": &form.Username, "password": &form.Password,
	} {
		if values, ok := query[key]; ok {
			*target = values[0]
		}
		query.Del(key)
	}
	query.Del("sslmode")
	if len(query) > 0 {
		form.Options = query
	}
	return form, true
}

func parsePgKeyValueDSN(dsn string) pgDSNForm {
	form := pgDSNForm{}
	settings := parsePgKeyValueSettings(dsn)
	form.Host = settings["host"]
	if form.Host == "" {
		form.Host = settings["hostaddr"]
	}
	for key, value := range settings {
		switch key {
		case "host", "hostaddr":
			// Resolved above so precedence does not depend on map iteration.
		case "port":
			form.Port = value
		case "database":
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
	// Multi-host and Unix socket addresses belong in the query, where URL
	// authority escaping cannot change their meaning.
	if strings.ContainsAny(host, ",/\\") || strings.Contains(port, ",") {
		u.Host = ""
		q.Set("host", host)
		q.Set("port", port)
	}
	q.Set("sslmode", sslmode)
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20")
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
		if key == "dbname" {
			key = "database"
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
