package store_test

import (
	"github.com/dcm-project/control-plane/internal/sp/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var _ = Describe("Store", func() {
	var db *gorm.DB

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("NewStore", func() {
		It("creates a store with service type instance access", func() {
			s := store.NewStore(db)

			Expect(s).NotTo(BeNil())
			Expect(s.ServiceTypeInstance()).NotTo(BeNil())
		})
	})

	Describe("Close", func() {
		It("closes the database connection", func() {
			s := store.NewStore(db)

			err := s.Close()

			Expect(err).NotTo(HaveOccurred())
		})
	})
})
