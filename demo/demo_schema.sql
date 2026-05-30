-- ============================================================================
-- demo_schema.sql — Comprehensive PostgreSQL data-type showcase
-- ============================================================================
-- Drop everything (clean start)
DROP SCHEMA IF EXISTS demo CASCADE;
CREATE SCHEMA demo;

-- --------------------------------------------------------------------------
-- 1. Custom types: enum, domain, composite
-- --------------------------------------------------------------------------
CREATE TYPE demo.mood AS ENUM ('happy', 'sad', 'neutral', 'excited', 'angry');

CREATE DOMAIN demo.email AS text
    CHECK (VALUE ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$');

CREATE DOMAIN demo.positive_int AS integer
    CHECK (VALUE > 0);

CREATE TYPE demo.address_composite AS (
    street  text,
    city    text,
    zip     text,
    country text
);

-- --------------------------------------------------------------------------
-- 2. Table: all_numeric_types
--    Covers: smallint, integer, bigint, decimal, numeric, real, double precision,
--            smallserial, serial, bigserial, money
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_numeric_types (
    id               bigserial PRIMARY KEY,
    col_smallint     smallint,
    col_integer      integer,
    col_bigint       bigint,
    col_decimal      decimal(14, 4),
    col_numeric      numeric(18, 6),
    col_real         real,
    col_double       double precision,
    col_smallserial  smallserial,
    col_serial       serial,
    col_money        money,
    col_positive_int demo.positive_int   -- domain
);

-- --------------------------------------------------------------------------
-- 3. Table: all_character_types
--    Covers: char(n), varchar(n), text, bpchar (blank-padded char), citext if loaded
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_character_types (
    id           serial PRIMARY KEY,
    col_char     char(10),
    col_varchar  varchar(255),
    col_text     text,
    col_bpchar   bpchar(10),            -- same as char(n)
    col_email    demo.email             -- domain with regex check
);

-- --------------------------------------------------------------------------
-- 4. Table: all_datetime_types
--    Covers: date, time, timetz, timestamp, timestamptz, interval
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_datetime_types (
    id             serial PRIMARY KEY,
    col_date       date,
    col_time       time,
    col_timetz     time with time zone,
    col_timestamp  timestamp,
    col_timestamptz timestamp with time zone,
    col_interval   interval
);

-- --------------------------------------------------------------------------
-- 5. Table: all_binary_bool_types
--    Covers: boolean, bytea, bit(n), bit varying(n)
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_binary_bool_types (
    id            serial PRIMARY KEY,
    col_boolean   boolean,
    col_bytea     bytea,
    col_bit8      bit(8),
    col_bit16     bit(16),
    col_varbit    bit varying(64)
);

-- --------------------------------------------------------------------------
-- 6. Table: all_network_types
--    Covers: inet, cidr, macaddr, macaddr8
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_network_types (
    id             serial PRIMARY KEY,
    col_inet       inet,
    col_cidr       cidr,
    col_macaddr    macaddr,
    col_macaddr8   macaddr8
);

-- --------------------------------------------------------------------------
-- 7. Table: all_json_types
--    Covers: json, jsonb
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_json_types (
    id        serial PRIMARY KEY,
    col_json  json,
    col_jsonb jsonb
);

-- --------------------------------------------------------------------------
-- 8. Table: all_uuid_oid_types
--    Covers: uuid, oid
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_uuid_oid_types (
    id       serial PRIMARY KEY,
    col_uuid uuid,
    col_oid  oid
);

-- --------------------------------------------------------------------------
-- 9. Table: all_geometric_types
--    Covers: point, line, lseg, box, path (closed/open), polygon, circle
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_geometric_types (
    id           serial PRIMARY KEY,
    col_point    point,
    col_line     line,
    col_lseg     lseg,
    col_box      box,
    col_path     path,
    col_polygon  polygon,
    col_circle   circle
);

-- --------------------------------------------------------------------------
-- 10. Table: all_range_types
--     Covers: int4range, int8range, numrange, tsrange, tstzrange, daterange
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_range_types (
    id              serial PRIMARY KEY,
    col_int4range   int4range,
    col_int8range   int8range,
    col_numrange    numrange,
    col_tsrange     tsrange,
    col_tstzrange   tstzrange,
    col_daterange   daterange
);

-- --------------------------------------------------------------------------
-- 11. Table: all_text_search_types
--     Covers: tsvector, tsquery
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_text_search_types (
    id            serial PRIMARY KEY,
    col_tsvector  tsvector,
    col_tsquery   tsquery
);

-- --------------------------------------------------------------------------
-- 12. Table: all_xml_pglsn_types
--     Covers: xml, pg_lsn
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_xml_pglsn_types (
    id          serial PRIMARY KEY,
    col_xml     xml,
    col_pg_lsn  pg_lsn
);

-- --------------------------------------------------------------------------
-- 13. Table: all_array_types
--     Covers: one-dimensional arrays of most scalar types
-- --------------------------------------------------------------------------
CREATE TABLE demo.all_array_types (
    id             serial PRIMARY KEY,
    col_int_arr    integer[],
    col_bigint_arr bigint[],
    col_real_arr   real[],
    col_text_arr   text[],
    col_varchar_arr varchar(100)[],
    col_bool_arr   boolean[],
    col_date_arr   date[],
    col_uuid_arr   uuid[],
    col_inet_arr   inet[],
    col_jsonb_arr  jsonb[],
    col_enum_arr   demo.mood[],
    col_numeric_arr numeric(10,2)[]
);

-- --------------------------------------------------------------------------
-- 14. Table: table_with_constraints
--     Covers: NOT NULL, UNIQUE, CHECK, DEFAULT, composite PK, FK
-- --------------------------------------------------------------------------
CREATE TABLE demo.orders (
    id            bigserial PRIMARY KEY,
    order_ref     varchar(50)  NOT NULL UNIQUE,
    customer_id   integer      NOT NULL DEFAULT 0,
    total_amount  numeric(12,2) NOT NULL CHECK (total_amount >= 0),
    status        text         NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending','shipped','delivered','cancelled')),
    placed_at     timestamptz  NOT NULL DEFAULT now(),
    notes         text
);

CREATE TABLE demo.order_lines (
    order_id   bigint NOT NULL REFERENCES demo.orders(id) ON DELETE CASCADE,
    line_no    smallint NOT NULL,
    product    text NOT NULL,
    quantity   integer NOT NULL CHECK (quantity > 0),
    unit_price numeric(10,2) NOT NULL CHECK (unit_price >= 0),
    PRIMARY KEY (order_id, line_no)
);

-- --------------------------------------------------------------------------
-- 15. Table using composite type
-- --------------------------------------------------------------------------
CREATE TABLE demo.contacts (
    id       serial PRIMARY KEY,
    name     text NOT NULL,
    address  demo.address_composite
);

-- --------------------------------------------------------------------------
-- 16. Table with generated / identity column (PG 10+)
-- --------------------------------------------------------------------------
CREATE TABLE demo.identity_demo (
    id          integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    description text
);

-- --------------------------------------------------------------------------
-- 17. Indexes (expression, partial, GIN for JSONB)
-- --------------------------------------------------------------------------
CREATE INDEX idx_orders_placed_at ON demo.orders (placed_at DESC);
CREATE INDEX idx_orders_customer ON demo.orders (customer_id) WHERE status <> 'cancelled';
CREATE INDEX idx_jsonb_data ON demo.all_json_types USING GIN (col_jsonb);
CREATE INDEX idx_tsvector ON demo.all_text_search_types USING GIN (col_tsvector);

-- --------------------------------------------------------------------------
-- 18. A simple view
-- --------------------------------------------------------------------------
CREATE OR REPLACE VIEW demo.order_summary AS
SELECT
    o.id,
    o.order_ref,
    o.status,
    o.total_amount,
    o.placed_at,
    COUNT(ol.line_no) AS line_count
FROM demo.orders o
LEFT JOIN demo.order_lines ol ON ol.order_id = o.id
GROUP BY o.id;

-- --------------------------------------------------------------------------
-- Verify everything was created
-- --------------------------------------------------------------------------
SELECT
    table_schema,
    table_name
FROM information_schema.tables
WHERE table_schema = 'demo'
  AND table_type = 'BASE TABLE'
ORDER BY table_name;
