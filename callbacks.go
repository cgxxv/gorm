package gorm

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jinzhu/gorm/logger"
	"github.com/jinzhu/gorm/schema"
	"github.com/jinzhu/gorm/utils"
)

func initializeCallbacks(db *DB) *callbacks {
	return &callbacks{
		processors: map[string]*processor{
			"create": &processor{db: db},
			"query":  &processor{db: db},
			"update": &processor{db: db},
			"delete": &processor{db: db},
			"row":    &processor{db: db},
			"raw":    &processor{db: db},
		},
	}
}

// callbacks gorm callbacks manager
type callbacks struct {
	processors map[string]*processor
}

type processor struct {
	db        *DB
	fns       []func(*DB)
	callbacks []*callback
}

type callback struct {
	name      string
	before    string
	after     string
	remove    bool
	replace   bool
	match     func(*DB) bool
	handler   func(*DB)
	processor *processor
}

func (cs *callbacks) Create() *processor {
	return cs.processors["create"]
}

func (cs *callbacks) Query() *processor {
	return cs.processors["query"]
}

func (cs *callbacks) Update() *processor {
	return cs.processors["update"]
}

func (cs *callbacks) Delete() *processor {
	return cs.processors["delete"]
}

func (cs *callbacks) Row() *processor {
	return cs.processors["row"]
}

func (cs *callbacks) Raw() *processor {
	return cs.processors["raw"]
}

func (p *processor) Execute(db *DB) {
	curTime := time.Now()
	if stmt := db.Statement; stmt != nil {
		if stmt.Model == nil {
			stmt.Model = stmt.Dest
		}

		if stmt.Model != nil {
			if err := stmt.Parse(stmt.Model); err != nil && (!errors.Is(err, schema.ErrUnsupportedDataType) || stmt.Table == "") {
				db.AddError(err)
			}
		}
		stmt.ReflectValue = reflect.Indirect(reflect.ValueOf(stmt.Dest))
	}

	for _, f := range p.fns {
		f(db)
	}

	if stmt := db.Statement; stmt != nil {
		db.Error = stmt.Error
		db.RowsAffected = stmt.RowsAffected

		db.Logger.Trace(curTime, func() (string, int64) {
			return db.Dialector.Explain(stmt.SQL.String(), stmt.Vars...), db.RowsAffected
		}, db.Error)
	}
}

func (p *processor) Get(name string) func(*DB) {
	for i := len(p.callbacks) - 1; i >= 0; i-- {
		if v := p.callbacks[i]; v.name == name && !v.remove {
			return v.handler
		}
	}
	return nil
}

func (p *processor) Before(name string) *callback {
	return &callback{before: name, processor: p}
}

func (p *processor) After(name string) *callback {
	return &callback{after: name, processor: p}
}

func (p *processor) Match(fc func(*DB) bool) *callback {
	return &callback{match: fc, processor: p}
}

func (p *processor) Register(name string, fn func(*DB)) error {
	return (&callback{processor: p}).Register(name, fn)
}

func (p *processor) Remove(name string) error {
	return (&callback{processor: p}).Remove(name)
}

func (p *processor) Replace(name string, fn func(*DB)) error {
	return (&callback{processor: p}).Replace(name, fn)
}

func (p *processor) compile() (err error) {
	var callbacks []*callback
	for _, callback := range p.callbacks {
		if callback.match == nil || callback.match(p.db) {
			callbacks = append(callbacks, callback)
		}
	}

	if p.fns, err = sortCallbacks(p.callbacks); err != nil {
		logger.Default.Error("Got error when compile callbacks, got %v", err)
	}
	return
}

func (c *callback) Before(name string) *callback {
	c.before = name
	return c
}

func (c *callback) After(name string) *callback {
	c.after = name
	return c
}

func (c *callback) Register(name string, fn func(*DB)) error {
	c.name = name
	c.handler = fn
	c.processor.callbacks = append(c.processor.callbacks, c)
	return c.processor.compile()
}

func (c *callback) Remove(name string) error {
	logger.Default.Warn("removing callback `%v` from %v\n", name, utils.FileWithLineNum())
	c.name = name
	c.remove = true
	c.processor.callbacks = append(c.processor.callbacks, c)
	return c.processor.compile()
}

func (c *callback) Replace(name string, fn func(*DB)) error {
	logger.Default.Info("replacing callback `%v` from %v\n", name, utils.FileWithLineNum())
	c.name = name
	c.handler = fn
	c.replace = true
	c.processor.callbacks = append(c.processor.callbacks, c)
	return c.processor.compile()
}

// getRIndex get right index from string slice
func getRIndex(strs []string, str string) int {
	for i := len(strs) - 1; i >= 0; i-- {
		if strs[i] == str {
			return i
		}
	}
	return -1
}

func sortCallbacks(cs []*callback) (fns []func(*DB), err error) {
	var (
		names, sorted []string
		sortCallback  func(*callback) error
	)

	for _, c := range cs {
		// show warning message the callback name already exists
		if idx := getRIndex(names, c.name); idx > -1 && !c.replace && !c.remove && !cs[idx].remove {
			logger.Default.Warn("duplicated callback `%v` from %v\n", c.name, utils.FileWithLineNum())
		}
		names = append(names, c.name)
	}

	sortCallback = func(c *callback) error {
		if c.before != "" { // if defined before callback
			if sortedIdx := getRIndex(sorted, c.before); sortedIdx != -1 {
				if curIdx := getRIndex(sorted, c.name); curIdx == -1 {
					// if before callback already sorted, append current callback just after it
					sorted = append(sorted[:sortedIdx], append([]string{c.name}, sorted[sortedIdx:]...)...)
				} else if curIdx > sortedIdx {
					return fmt.Errorf("conflicting callback %v with before %v", c.name, c.before)
				}
			} else if idx := getRIndex(names, c.before); idx != -1 {
				// if before callback exists
				cs[idx].after = c.name
			}
		}

		if c.after != "" { // if defined after callback
			if sortedIdx := getRIndex(sorted, c.after); sortedIdx != -1 {
				if curIdx := getRIndex(sorted, c.name); curIdx == -1 {
					// if after callback sorted, append current callback to last
					sorted = append(sorted, c.name)
				} else if curIdx < sortedIdx {
					return fmt.Errorf("conflicting callback %v with before %v", c.name, c.after)
				}
			} else if idx := getRIndex(names, c.after); idx != -1 {
				// if after callback exists but haven't sorted
				// set after callback's before callback to current callback
				after := cs[idx]

				if after.before == "" {
					after.before = c.name
				}

				if err := sortCallback(after); err != nil {
					return err
				}

				if err := sortCallback(c); err != nil {
					return err
				}
			}
		}

		// if current callback haven't been sorted, append it to last
		if getRIndex(sorted, c.name) == -1 {
			sorted = append(sorted, c.name)
		}

		return nil
	}

	for _, c := range cs {
		if err = sortCallback(c); err != nil {
			return
		}
	}

	for _, name := range sorted {
		if idx := getRIndex(names, name); !cs[idx].remove {
			fns = append(fns, cs[idx].handler)
		}
	}

	return
}
