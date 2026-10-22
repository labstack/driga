package service

import (
	"bytes"
	"html"
	"sort"
	"text/template"

	ks "github.com/kardianos/service"
)

func renderLaunchd(c *ks.Config) ([]byte, error) {
	var env []string
	for key, value := range c.EnvVars {
		env = append(env, "<key>"+html.EscapeString(key)+"</key><string>"+html.EscapeString(value)+"</string>")
	}
	sort.Strings(env)
	t, err := template.New("launchd").Funcs(template.FuncMap{
		"Name":      func() string { return c.Name },
		"Path":      func() string { return c.Executable },
		"Arguments": func() []string { return c.Arguments },
		"EnvVars":   func() []string { return env },
		"KeepAlive": func() string { return "true" },
		"RunAtLoad": func() string { return "true" },
	}).Parse(c.Option["LaunchdConfig"].(string))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	err = t.Execute(&out, nil)
	return out.Bytes(), err
}
