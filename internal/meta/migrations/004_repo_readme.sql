-- A Markdown README per repository, written in the UI (or copied from GitHub).
ALTER TABLE repositories ADD COLUMN readme TEXT NOT NULL DEFAULT '';
