package platform

import "sync"

// identKey distinguishes one process image: exec keeps the pid and start time
// but replaces the kernel command name.
type identKey struct {
	start int64
	comm  string
}

type ident struct {
	key  identKey
	argv []string
	exe  string
}

// identCache keeps argv and executable path across scans; both are fixed for a
// process image, and reading argv also copies the process environment.
type identCache struct {
	mu sync.Mutex
	m  map[int32]ident
}

func (c *identCache) get(pid int32, key identKey, load func() ([]string, string, error)) ([]string, string, error) {
	c.mu.Lock()
	v, ok := c.m[pid]
	c.mu.Unlock()
	if ok && v.key == key {
		return v.argv, v.exe, nil
	}
	argv, exe, err := load()
	if err != nil {
		return nil, "", err
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[int32]ident)
	}
	c.m[pid] = ident{key: key, argv: argv, exe: exe}
	c.mu.Unlock()
	return argv, exe, nil
}

// retain drops processes absent from the latest scan.
func (c *identCache) retain(seen map[int32]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for pid := range c.m {
		if _, ok := seen[pid]; !ok {
			delete(c.m, pid)
		}
	}
}
