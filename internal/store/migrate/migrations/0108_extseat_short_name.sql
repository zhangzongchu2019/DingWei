-- ㊿-补29：只放宽外部席位短名。旧行不改；禁止有长名时恢复旧 CHECK。
ALTER TABLE np_extseat DROP CONSTRAINT np_extseat_short_name_check;
ALTER TABLE np_extseat ADD CONSTRAINT np_extseat_short_name_check
 CHECK(short_name ~ '^[a-z0-9]{1,20}$');
