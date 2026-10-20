-- +goose Up
-- +goose StatementBegin
-- image_assets 保存资源中心图片的稳定标识、显示名与不可变磁盘文件名。
CREATE TABLE IF NOT EXISTS image_assets (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    file_name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_image_assets_name ON image_assets(name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_image_assets_file_name ON image_assets(file_name);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_image_assets_file_name;
DROP INDEX IF EXISTS idx_image_assets_name;
DROP TABLE IF EXISTS image_assets;
-- +goose StatementEnd
