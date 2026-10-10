-- +goose Up
-- 用户主动停止订单全部自动化；记录独立于规则和运行，重启及删除规则都不能解除。
CREATE TABLE order_automation_stops (
 cookie_id TEXT NOT NULL,
 order_id TEXT NOT NULL,
 stopped_by BIGINT NOT NULL,
 stopped_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY (cookie_id, order_id),
 FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
-- +goose Down
DROP TABLE order_automation_stops;
