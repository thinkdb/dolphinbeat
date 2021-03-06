// Copyright 2019 ByteWatch All Rights Reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//    http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package schema

import (
	"database/sql"
	"fmt"
	"github.com/bytewatch/dolphinbeat/canal/prog"
	_ "github.com/go-sql-driver/mysql"
	"github.com/siddontang/go-log/log"
)

var (
	tableName string = "tb_schema_data"
	initQuery string = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s ( 
		id INT NOT NULL AUTO_INCREMENT, 
		server_id INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'server id of mysql master', 
		name VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'binlog name', 
		pos INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'binlog pos', 
		snapshot LONGBLOB NOT NULL COMMENT 'snapshot of schema', 
		statement LONGBLOB NOT NULL COMMENT 'ddl statement', 
		type ENUM('snapshot','statement') NOT NULL DEFAULT 'snapshot' COMMENT 'snapshot or statement', 
		time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'create time of this record',
		PRIMARY KEY(id)
	)`, tableName)
)

type MysqlStorage struct {
	dsn string
}

func NewMysqlStorage(addr string, user string, password string, database string) (*MysqlStorage, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s", user, password, addr, database)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	_, err = db.Exec(initQuery)
	if err != nil {
		log.Errorf("create table error: %s", err)
		return nil, err
	}

	storage := &MysqlStorage{
		dsn: dsn,
	}
	return storage, nil
}

func (o *MysqlStorage) SaveSnapshot(data []byte, pos prog.Position) error {
	var err error
	db, err := sql.Open("mysql", o.dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	query := fmt.Sprintf(
		"INSERT INTO %s SET server_id=?, name=?, pos=?, snapshot=?, statement='', type = ?",
		tableName)
	_, err = tx.Exec(query, pos.ServerID, pos.Name, pos.Pos, data, "snapshot")
	if err != nil {
		log.Errorf("insert into db error: %s", err)
		return err
	}

	// Purge expired data
	err = o.purge(tx)
	if err != nil {
		log.Errorf("purge data error: %s", err)
		return err
	}

	err = tx.Commit()
	if err != nil {
		log.Errorf("commit transaction error: %s", err)
		return err
	}

	return nil
}

func (o *MysqlStorage) LoadLastSnapshot() ([]byte, prog.Position, error) {
	var pos prog.Position
	var data []byte

	db, err := sql.Open("mysql", o.dsn)
	if err != nil {
		return nil, pos, err
	}
	defer db.Close()

	query := fmt.Sprintf(
		"SELECT server_id, name, pos, snapshot FROM %s WHERE type='snapshot' ORDER BY id DESC LIMIT 1 ",
		tableName)
	row := db.QueryRow(query)
	err = row.Scan(&pos.ServerID, &pos.Name, &pos.Pos, &data)
	if err != nil && err != sql.ErrNoRows {
		log.Errorf("query from db error: %s", err)
		return nil, pos, err
	}

	return data, pos, nil
}

func (o *MysqlStorage) SaveStatement(db string, statement string, pos prog.Position) error {
	return nil
}

func (o *MysqlStorage) LoadNextStatement(prePos prog.Position) (string, string, prog.Position, error) {
	var pos prog.Position
	return "", "", pos, nil
}

func (o *MysqlStorage) Reset() error {
	db, err := sql.Open("mysql", o.dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	sql := fmt.Sprintf("DELETE FROM `%s`", tableName)
	_, err = db.Exec(sql)
	if err != nil {
		log.Errorf("insert into db error: %s", err)
		return err
	}

	if err != nil {
		return err
	}

	return nil
}

// Purge the snapshot or statement before the last snapshot
func (o *MysqlStorage) purge(tx *sql.Tx) error {
	var err error
	var lastSnapshotId int
	query := fmt.Sprintf(
		"SELECT id FROM %s WHERE type='snapshot' ORDER BY id DESC LIMIT 1 ",
		tableName)
	row := tx.QueryRow(query)
	err = row.Scan(&lastSnapshotId)
	if err != nil && err != sql.ErrNoRows {
		log.Errorf("query from db error: %s", err)
		return err
	}
	query = fmt.Sprintf("DELETE FROM %s WHERE id < ? AND datediff(curdate(),create_time) >= 7", tableName)
	_, err = tx.Exec(query, lastSnapshotId)
	if err != nil {
		log.Errorf("delete from db error: %s", err)
		return err
	}

	return nil

}
