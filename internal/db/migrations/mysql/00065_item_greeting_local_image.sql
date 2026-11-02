-- +goose Up
-- 商品首次回复复用安全本地图片引用，历史 URL 规则保持空路径。
ALTER TABLE item_greeting_rules ADD COLUMN reply_image_path VARCHAR(1024) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE item_greeting_rules DROP COLUMN reply_image_path;
