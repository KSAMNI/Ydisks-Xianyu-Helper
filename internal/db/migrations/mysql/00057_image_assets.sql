-- +goose Up
-- +goose StatementBegin
-- image_assets 保存资源中心图片的稳定标识、显示名与不可变磁盘文件名。
-- name 使用 utf8mb4 唯一索引，长度上限保证索引字节数在旧版 InnoDB 限制内。
CREATE TABLE IF NOT EXISTS image_assets (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(191) NOT NULL,
    file_name VARCHAR(128) NOT NULL,
    mime_type VARCHAR(64) NOT NULL,
    size_bytes BIGINT NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_image_assets_name (name),
    UNIQUE KEY uk_image_assets_file_name (file_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS image_assets;
-- +goose StatementEnd
