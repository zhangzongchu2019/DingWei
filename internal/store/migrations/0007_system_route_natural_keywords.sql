-- R9: natural-language system schedule keywords. action=record|coordinate|auto.
ALTER TABLE system_route ADD COLUMN action TEXT NOT NULL DEFAULT 'auto';

UPDATE system_route SET action='auto' WHERE keyword='sys:调度';

INSERT OR IGNORE INTO system_route(keyword, service_name, action, priority, active) VALUES
('#日程上报','scheduler','record',10,1),
('#日程提交','scheduler','record',10,1),
('#上报日程','scheduler','record',10,1),
('#提交日程','scheduler','record',10,1),
('#工作上报','scheduler','record',10,1),
('#上报工作','scheduler','record',10,1),
('#工作提交','scheduler','record',10,1),
('#提交工作','scheduler','record',10,1),
('#工作汇报','scheduler','record',10,1),
('#汇报工作','scheduler','record',10,1),
('#汇报进度','scheduler','record',10,1),
('#进度汇报','scheduler','record',10,1),
('#修改日程','scheduler','coordinate',20,1),
('#日程修改','scheduler','coordinate',20,1),
('#工作修改','scheduler','coordinate',20,1),
('#修改工作','scheduler','coordinate',20,1),
('#进度修改','scheduler','coordinate',20,1),
('#修改进度','scheduler','coordinate',20,1),
('#工作调整','scheduler','coordinate',20,1),
('#调整工作','scheduler','coordinate',20,1),
('#日程调整','scheduler','coordinate',20,1),
('#调整日程','scheduler','coordinate',20,1),
('#调整进度','scheduler','coordinate',20,1),
('#进度调整','scheduler','coordinate',20,1);
