package store

import _ "embed"

// SchemaSQL 是 SQLite store 的 schema 契约。修改 schema.sql 时调用方会同步拿到
// 同一份建表语句,避免实现里复制 SQL。
//
//go:embed schema.sql
var SchemaSQL string
