-- +goose Up
-- 卖家侧评价事实：订单完成后本系统已通过评价接口给出评价，或平台明确回答该订单已评价过。
-- 该列只由自动评价任务在外部评价动作成功收口时写入一次（RFC3339 UTC 文本），
-- 作为自动评价候选的唯一排除依据，避免历史已评价订单长期占用按完成时间排序的候选窗口。
ALTER TABLE orders ADD COLUMN seller_rated_at TEXT DEFAULT '';
-- +goose Down
ALTER TABLE orders DROP COLUMN seller_rated_at;
