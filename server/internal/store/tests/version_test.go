// Тесты номера схемы, которую эта сборка умеет создать.
package store_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

func TestVersionIsTheHighestMigrationInTheBuild(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "migrations"))
	if err != nil {
		t.Fatalf("каталог миграций: %v", err)
	}

	want := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		num, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		if v > want {
			want = v
		}
	}

	if got := store.Version(); got != want {
		t.Fatalf("Version() = %d, каталог миграций говорит %d", got, want)
	}
	if want == 0 {
		t.Fatal("в сборке не осталось миграций, порядок запуска утрачен")
	}
}
