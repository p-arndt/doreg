-- Projects can refuse pushes that move an existing tag to another manifest.
ALTER TABLE projects ADD COLUMN immutable_tags BIGINT NOT NULL DEFAULT 0;
