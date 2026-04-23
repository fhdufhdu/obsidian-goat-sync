package sqlite

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
		create table file {
			id integer PRIMARY KEY AUTOINCREMENT;
			vault_id integer not null;
			path varchar(256) not null UNIQUE;
			version integer default 1 not null;
			hash text not null;
			is_deleted boolean default false not null;
			updated_at timestamp default now() not null;
			created_at timestamp default now() not null;
		}

		create table vault {
			id integer PRIMARY KEY AUTOINCREMENT;
			name varchar(256) not null UNIQUE;
			is_deleted boolean default false not null;
			updated_at timestamp default now() not null;
			created_at timestamp default now() not null;
		}
	`); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
