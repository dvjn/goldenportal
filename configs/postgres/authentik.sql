-- Creates Authentik's role and database in the dev Postgres when they are
-- missing. deps:up runs it through psql with the variables user, password
-- and database set.
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'user', :'password')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'user')
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', :'database', :'user')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'database')
\gexec
