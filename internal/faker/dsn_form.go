package faker

import (
	"fmt"
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
	for part := range strings.SplitSeq(dsn, " ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		value = strings.TrimSpace(value)
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
	password := strings.TrimSpace(form.Password)
	sslmode := strings.TrimSpace(form.SSLMode)
	if sslmode == "" {
		sslmode = pgDefaultSSLMode
	}

	u := &url.URL{
		Scheme: pgSchemePostgres,
		Host:   host + ":" + port,
		Path:   "/" + database,
	}
	if username != "" || password != "" {
		u.User = url.UserPassword(username, password)
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
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
	return fmt.Sprintf("%s/%s", host, db)
}
