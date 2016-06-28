package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/kaneshin/kamimai/core/internal/cast"
	"github.com/kaneshin/kamimai/core/internal/direction"
	"github.com/kaneshin/kamimai/core/internal/version"
)

const (
	notFoundIndex = 0xffff
)

var (
	errOutOfBoundsMigrations = errors.New("out of bounds migration")
)

type (
	// A Service manages for kamimai.
	Service struct {
		config    *Config
		driver    Driver
		version   uint64
		direction int
		data      Migrations
	}
)

func (s Service) walker(indexPath map[uint64]*Migration) func(path string, info os.FileInfo, err error) error {
	wd, _ := os.Getwd()

	return func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		name := info.Name()
		ver := cast.Uint64(version.Get(name))
		mig, found := indexPath[ver]
		if !found || mig == nil {
			mig = &Migration{
				version: ver,
			}
			indexPath[ver] = mig
		}

		if s.direction == direction.Get(name) {
			mig.name = filepath.Clean(filepath.Join(wd, path))
		} else if s.direction == direction.Unknown {
			mig.name = filepath.Clean(filepath.Join(wd, path))
		}

		return nil
	}
}

// NewService returns a new Service pointer that can be chained with builder methods to
// set multiple configuration values inline without using pointers.
func NewService(c *Config) *Service {
	svc := &Service{
		config: c,
	}
	return svc
}

// WithVersion sets a config version value returning a Service pointer
// for chaining.
func (s *Service) WithVersion(v interface{}) *Service {
	s.version = cast.Uint64(v)
	return s
}

// WithDriver sets a driver returning a Service pointer for chaining.
func (s *Service) WithDriver(d Driver) *Service {
	s.driver = d
	return s
}

// MakeMigrationsDir creates a directory named path, along with any necessary
// parents, and returns nil, or else returns an error. If path is
// already a directory, MkdirAll does nothing and returns nil.
func (s *Service) MakeMigrationsDir() error {
	return os.MkdirAll(s.config.migrationsDir(), 0777)
}

func (s *Service) apply() {
	index := map[uint64]*Migration{}
	filepath.Walk(s.config.migrationsDir(), s.walker(index))

	list := make([]*Migration, len(index))
	i := 0
	for _, m := range index {
		list[i] = m
		i++
	}

	migs := Migrations(list)
	sort.Sort(migs)
	s.data = migs
}

func (s *Service) do(idx int) error {
	migs := s.data
	if !(0 <= idx && idx < migs.Len()) {
		return errOutOfBoundsMigrations
	}
	return s.driver.Migrate(migs[idx])
}

func (s *Service) step(n int) error {
	s.apply()

	// gets current index of migrations
	idx := s.data.index(Migration{version: s.version})
	if s.version == 0 {
		idx = 0
	}

	// direction of the migration.
	sign := -1
	if n > 0 {
		sign = 1
	}

	for i := 0; i < sign*n; i++ {
		if err := s.do(idx + sign*i); err != nil {
			return err
		}
	}

	return nil
}

// Up upgrades migration version.
func (s *Service) Up() error {
	s.direction = direction.Up
	err := s.step(notFoundIndex)
	switch err {
	case errOutOfBoundsMigrations:
		return nil
	}
	return err
}

// Down downgrades migration version.
func (s *Service) Down() error {
	s.direction = direction.Down
	err := s.step(-notFoundIndex)
	switch err {
	case errOutOfBoundsMigrations:
		return nil
	}
	return err
}

// Next upgrades migration version.
func (s *Service) Next() error {
	s.direction = direction.Up
	return s.step(1)
}

// Prev downgrades migration version.
func (s *Service) Prev() error {
	s.direction = direction.Down
	return s.step(-1)
}

// NextMigration returns next version migrations.
func (s *Service) NextMigration(name string) (up *Migration, down *Migration, err error) {
	s.apply()

	// initialize default variables for making migrations.
	up, down = &Migration{version: 1, name: ""}, &Migration{version: 1, name: ""}
	ver := "001"

	// gets the oldest migration version file.
	if obj := s.data.last(); obj != nil {
		// for version number
		v := obj.version + 1
		up.version, down.version = v, v
	}
	if obj := s.data.first(); obj != nil {
		// for version format
		_, file := filepath.Split(obj.name)
		ver = version.Format(file)

		if _, err := time.Parse("20060102150405", version.Get(file)); err == nil {
			v := cast.Uint64(time.Now())
			up.version, down.version = v, v
		}

	}

	// [ver]_[name]_[direction-suffix][.ext]
	base := fmt.Sprintf("%s_%s_%%s%%s", ver, name)
	// including dot
	ext := s.driver.Ext()

	// up
	n := fmt.Sprintf(base, up.version, direction.Suffix(direction.Up), ext)
	up.name = filepath.Join(s.config.migrationsDir(), n)
	// down
	n = fmt.Sprintf(base, down.version, direction.Suffix(direction.Down), ext)
	down.name = filepath.Join(s.config.migrationsDir(), n)

	return
}
