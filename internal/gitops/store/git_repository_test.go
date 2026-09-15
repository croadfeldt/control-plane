package store_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/dcm-project/control-plane/internal/gitops/store"
	"github.com/dcm-project/control-plane/internal/gitops/store/model"
)

var _ = Describe("GitRepositoryStore", func() {
	var (
		db        *gorm.DB
		dataStore store.Store
		ctx       context.Context
	)

	BeforeEach(func() {
		var err error
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(db.AutoMigrate(&model.GitRepository{}, &model.ManagedInstance{})).To(Succeed())
		dataStore = store.NewStore(db)
		ctx = context.Background()
	})

	Describe("Create", func() {
		It("creates a git repository", func() {
			repo := model.GitRepository{
				ID:          "test-repo",
				ApiVersion:  "v1alpha1",
				DisplayName: "Test Repo",
				URL:         "https://example.com/repo.git",
				Branch:      "main",
				Path:        ".",
			}
			created, err := dataStore.GitRepository().Create(ctx, repo)
			Expect(err).NotTo(HaveOccurred())
			Expect(created.ID).To(Equal("test-repo"))
			Expect(created.DisplayName).To(Equal("Test Repo"))
			Expect(created.SyncState).To(Equal("PENDING"))
		})

		It("returns error on duplicate ID", func() {
			repo := model.GitRepository{
				ID:          "dup-id",
				ApiVersion:  "v1alpha1",
				DisplayName: "First",
				URL:         "https://example.com/repo.git",
			}
			_, err := dataStore.GitRepository().Create(ctx, repo)
			Expect(err).NotTo(HaveOccurred())

			repo2 := model.GitRepository{
				ID:          "dup-id",
				ApiVersion:  "v1alpha1",
				DisplayName: "Second",
				URL:         "https://example.com/repo2.git",
			}
			_, err = dataStore.GitRepository().Create(ctx, repo2)
			Expect(err).To(MatchError(store.ErrGitRepositoryIDTaken))
		})

		It("returns error on duplicate display_name", func() {
			repo := model.GitRepository{
				ID:          "repo-1",
				ApiVersion:  "v1alpha1",
				DisplayName: "Same Name",
				URL:         "https://example.com/repo.git",
			}
			_, err := dataStore.GitRepository().Create(ctx, repo)
			Expect(err).NotTo(HaveOccurred())

			repo2 := model.GitRepository{
				ID:          "repo-2",
				ApiVersion:  "v1alpha1",
				DisplayName: "Same Name",
				URL:         "https://example.com/repo2.git",
			}
			_, err = dataStore.GitRepository().Create(ctx, repo2)
			Expect(err).To(MatchError(store.ErrDisplayNameTaken))
		})
	})

	Describe("Get", func() {
		It("returns the repository", func() {
			repo := model.GitRepository{
				ID:          "get-test",
				ApiVersion:  "v1alpha1",
				DisplayName: "Get Test",
				URL:         "https://example.com/repo.git",
			}
			_, err := dataStore.GitRepository().Create(ctx, repo)
			Expect(err).NotTo(HaveOccurred())

			got, err := dataStore.GitRepository().Get(ctx, "get-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.DisplayName).To(Equal("Get Test"))
		})

		It("returns not found for non-existent ID", func() {
			_, err := dataStore.GitRepository().Get(ctx, "nonexistent")
			Expect(err).To(MatchError(store.ErrGitRepositoryNotFound))
		})
	})

	Describe("List", func() {
		BeforeEach(func() {
			for _, id := range []string{"aaa", "bbb", "ccc"} {
				_, err := dataStore.GitRepository().Create(ctx, model.GitRepository{
					ID:          id,
					ApiVersion:  "v1alpha1",
					DisplayName: "Repo " + id,
					URL:         "https://example.com/" + id + ".git",
				})
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("lists all repositories", func() {
			result, err := dataStore.GitRepository().List(ctx, &store.GitRepositoryListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.GitRepositories).To(HaveLen(3))
			Expect(result.NextPageToken).To(BeEmpty())
		})

		It("supports pagination", func() {
			result, err := dataStore.GitRepository().List(ctx, &store.GitRepositoryListOptions{PageSize: 2})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.GitRepositories).To(HaveLen(2))
			Expect(result.NextPageToken).NotTo(BeEmpty())

			result2, err := dataStore.GitRepository().List(ctx, &store.GitRepositoryListOptions{
				PageToken: &result.NextPageToken,
				PageSize:  2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result2.GitRepositories).To(HaveLen(1))
			Expect(result2.NextPageToken).To(BeEmpty())
		})
	})

	Describe("Delete", func() {
		It("deletes an existing repository", func() {
			_, err := dataStore.GitRepository().Create(ctx, model.GitRepository{
				ID:          "del-test",
				ApiVersion:  "v1alpha1",
				DisplayName: "Del Test",
				URL:         "https://example.com/repo.git",
			})
			Expect(err).NotTo(HaveOccurred())

			err = dataStore.GitRepository().Delete(ctx, "del-test")
			Expect(err).NotTo(HaveOccurred())

			_, err = dataStore.GitRepository().Get(ctx, "del-test")
			Expect(err).To(MatchError(store.ErrGitRepositoryNotFound))
		})

		It("returns not found for non-existent ID", func() {
			err := dataStore.GitRepository().Delete(ctx, "nonexistent")
			Expect(err).To(MatchError(store.ErrGitRepositoryNotFound))
		})
	})

	Describe("UpdateSyncStatus", func() {
		It("updates sync status fields", func() {
			_, err := dataStore.GitRepository().Create(ctx, model.GitRepository{
				ID:          "sync-test",
				ApiVersion:  "v1alpha1",
				DisplayName: "Sync Test",
				URL:         "https://example.com/repo.git",
			})
			Expect(err).NotTo(HaveOccurred())

			err = dataStore.GitRepository().UpdateSyncStatus(ctx, "sync-test", "SYNCED", "All good", "abc123")
			Expect(err).NotTo(HaveOccurred())

			got, err := dataStore.GitRepository().Get(ctx, "sync-test")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.SyncState).To(Equal("SYNCED"))
			Expect(got.StatusMessage).To(Equal("All good"))
			Expect(got.LastSyncedCommit).To(Equal("abc123"))
			Expect(got.LastSyncTime).NotTo(BeNil())
		})

		It("returns not found for non-existent ID", func() {
			err := dataStore.GitRepository().UpdateSyncStatus(ctx, "nonexistent", "SYNCED", "", "")
			Expect(err).To(MatchError(store.ErrGitRepositoryNotFound))
		})
	})

	Describe("Update", func() {
		It("updates mutable fields", func() {
			_, err := dataStore.GitRepository().Create(ctx, model.GitRepository{
				ID:              "upd-test",
				ApiVersion:      "v1alpha1",
				DisplayName:     "Original",
				URL:             "https://example.com/repo.git",
				Branch:          "main",
				IntervalSeconds: 60,
			})
			Expect(err).NotTo(HaveOccurred())

			updated, err := dataStore.GitRepository().Update(ctx, model.GitRepository{
				ID:              "upd-test",
				DisplayName:     "Updated",
				URL:             "https://example.com/new-repo.git",
				Branch:          "develop",
				Path:            "apps/",
				IntervalSeconds: 30,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.DisplayName).To(Equal("Updated"))
			Expect(updated.URL).To(Equal("https://example.com/new-repo.git"))
			Expect(updated.Branch).To(Equal("develop"))
			Expect(updated.IntervalSeconds).To(Equal(30))
		})

		It("returns not found for non-existent ID", func() {
			_, err := dataStore.GitRepository().Update(ctx, model.GitRepository{
				ID:          "nonexistent",
				DisplayName: "Test",
				URL:         "https://example.com/repo.git",
			})
			Expect(err).To(MatchError(store.ErrGitRepositoryNotFound))
		})
	})
})
