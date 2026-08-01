package gormdb

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// sqliteMigrator overrides information_schema-based checks from the generic
// GORM migrator with sqlite_master queries. Without these, AutoMigrate always
// believes tables/indexes are missing and fails with
// "table `…` already exists" on the second open — which made API key init fall
// back to an empty in-memory store after every restart (stargate-6wu).
type sqliteMigrator struct {
	migrator.Migrator
}

func (d moderncSQLiteDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return sqliteMigrator{migrator.Migrator{Config: migrator.Config{
		DB:                          db,
		Dialector:                   d,
		CreateIndexAfterCreateTable: true,
	}}}
}

func (m sqliteMigrator) CurrentDatabase() (name string) {
	var null interface{}
	_ = m.DB.Raw("PRAGMA database_list").Row().Scan(&null, &name, &null)
	return
}

func (m sqliteMigrator) GetTables() (tableList []string, err error) {
	err = m.DB.Raw("SELECT name FROM sqlite_master WHERE type = ?", "table").Scan(&tableList).Error
	return
}

func (m sqliteMigrator) HasTable(value interface{}) bool {
	var count int64
	_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		return m.DB.Raw(
			"SELECT count(*) FROM sqlite_master WHERE type = ? AND name = ?",
			"table", stmt.Table,
		).Scan(&count).Error
	})
	return count > 0
}

func (m sqliteMigrator) DropTable(values ...interface{}) error {
	values = m.ReorderModels(values, false)
	for i := len(values) - 1; i >= 0; i-- {
		if err := m.RunWithValue(values[i], func(stmt *gorm.Statement) error {
			return m.DB.Exec("DROP TABLE IF EXISTS ?", clause.Table{Name: stmt.Table}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m sqliteMigrator) HasColumn(value interface{}, name string) bool {
	var count int64
	_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if field := stmt.Schema.LookUpField(name); field != nil {
				name = field.DBName
			}
		}
		// PRAGMA table_info is reliable regardless of how CREATE TABLE quoted columns.
		type col struct {
			Name string
		}
		var cols []col
		if err := m.DB.Raw("PRAGMA table_info(?)", clause.Table{Name: stmt.Table}).Scan(&cols).Error; err != nil {
			return err
		}
		for _, c := range cols {
			if c.Name == name {
				count = 1
				break
			}
		}
		return nil
	})
	return count > 0
}

func (m sqliteMigrator) HasIndex(value interface{}, name string) bool {
	var count int64
	_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if idx := stmt.Schema.LookIndex(name); idx != nil {
				name = idx.Name
			}
		}
		if name == "" {
			return nil
		}
		return m.DB.Raw(
			"SELECT count(*) FROM sqlite_master WHERE type = ? AND tbl_name = ? AND name = ?",
			"index", stmt.Table, name,
		).Scan(&count).Error
	})
	return count > 0
}

func (m sqliteMigrator) CreateIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema == nil {
			return fmt.Errorf("failed to create index with name %v", name)
		}
		idx := stmt.Schema.LookIndex(name)
		if idx == nil {
			return fmt.Errorf("failed to create index with name %v", name)
		}
		opts := m.BuildIndexOptions(idx.Fields, stmt)
		values := []interface{}{clause.Column{Name: idx.Name}, clause.Table{Name: stmt.Table}, opts}

		createIndexSQL := "CREATE "
		if idx.Class != "" {
			createIndexSQL += idx.Class + " "
		}
		// IF NOT EXISTS: safe when HasIndex is wrong or index was created outside GORM.
		createIndexSQL += "INDEX IF NOT EXISTS ? ON ??"
		if idx.Where != "" {
			createIndexSQL += " WHERE " + idx.Where
		}
		return m.DB.Exec(createIndexSQL, values...).Error
	})
}

func (m sqliteMigrator) DropIndex(value interface{}, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if idx := stmt.Schema.LookIndex(name); idx != nil {
				name = idx.Name
			}
		}
		// SQLite: DROP INDEX name (no ON table clause).
		return m.DB.Exec("DROP INDEX IF EXISTS ?", clause.Column{Name: name}).Error
	})
}

// BuildIndexOptions mirrors gorm.io/driver/sqlite so CREATE INDEX column lists are correct.
func (m sqliteMigrator) BuildIndexOptions(opts []schema.IndexOption, stmt *gorm.Statement) (results []interface{}) {
	for _, opt := range opts {
		str := stmt.Quote(opt.DBName)
		if opt.Expression != "" {
			str = opt.Expression
		}
		if opt.Collate != "" {
			str += " COLLATE " + opt.Collate
		}
		if opt.Sort != "" {
			str += " " + opt.Sort
		}
		results = append(results, clause.Expr{SQL: str})
	}
	return
}

// AlterColumn is a no-op on SQLite. Generic GORM emits PostgreSQL-style
// "ALTER COLUMN … TYPE …" which SQLite rejects ("near TYPE: syntax error").
// Full column type changes require table rebuild (see gorm.io/driver/sqlite);
// Stargate schemas are stable and only need create-if-missing + add-column.
func (m sqliteMigrator) AlterColumn(value interface{}, name string) error {
	return nil
}

// MigrateColumn skips smart type/default rewrites on SQLite (see AlterColumn).
func (m sqliteMigrator) MigrateColumn(value interface{}, field *schema.Field, columnType gorm.ColumnType) error {
	return nil
}
