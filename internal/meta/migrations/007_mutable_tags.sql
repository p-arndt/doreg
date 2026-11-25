-- Tag patterns (comma-separated, shell globs such as latest or *-dev) that
-- stay movable in a project with immutable tags.
ALTER TABLE projects ADD COLUMN mutable_tags TEXT NOT NULL DEFAULT '';
