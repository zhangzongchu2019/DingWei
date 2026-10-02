-- 可重试 failed 的连续次数（组长取甲：失败重投指数退避＋到限转 dead）。成功（injected／processed）即清零。
ALTER TABLE session_delivery ADD COLUMN failed_count INTEGER NOT NULL DEFAULT 0;
