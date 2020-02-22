package postgres_test

import (
	"fmt"
	"testing"

	"github.com/jinzhu/gorm"
	"github.com/jinzhu/gorm/dialects/postgres"
	"github.com/jinzhu/gorm/tests"
)

var (
	DB  *gorm.DB
	err error
)

func init() {
	if DB, err = gorm.Open(postgres.Open("user=gorm password=gorm DB.name=gorm port=9920 sslmode=disable"), &gorm.Config{}); err != nil {
		panic(fmt.Sprintf("failed to initialize database, got error %v", err))
	}
}

func TestCURD(t *testing.T) {
	tests.RunTestsSuit(t, DB)
}

func TestMigrate(t *testing.T) {
	tests.TestMigrate(t, DB)
}
