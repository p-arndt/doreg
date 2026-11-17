-- Pull counters. A manifest counts every GET; a repository counts only pulls
-- of top-level manifests, so one multi-platform pull (index, then platform
-- manifest) counts once.
ALTER TABLE manifests ADD COLUMN pull_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE repositories ADD COLUMN pull_count BIGINT NOT NULL DEFAULT 0;
