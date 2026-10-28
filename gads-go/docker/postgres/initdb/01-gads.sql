-- Run once by the postgres image when its data volume is first initialised
-- (/docker-entrypoint-initdb.d), connected to POSTGRES_DB as POSTGRES_USER.
-- GADS stores times without a zone, in UTC, and its text as UTF-8.
ALTER DATABASE :"DBNAME" SET timezone TO 'UTC';
ALTER DATABASE :"DBNAME" SET client_encoding TO 'UTF8';
