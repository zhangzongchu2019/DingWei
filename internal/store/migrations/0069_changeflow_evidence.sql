-- R45 first provider-neutral R1/R8 core. 0067 and 0068 remain byte-for-byte intact.
-- State and its append-only revision/result/event history are one CAS aggregate.
CREATE TABLE changeflow_evidence (
 coordinate_key TEXT PRIMARY KEY CHECK(length(coordinate_key)=64),
 revision INTEGER NOT NULL CHECK(revision>0),
 body TEXT NOT NULL CHECK(length(body)>0 AND length(body)<=8388608)
);
