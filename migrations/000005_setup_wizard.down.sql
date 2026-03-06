-- Remove setup wizard config keys.
DELETE FROM admin_config WHERE key IN (
    'jwt_secret', 'tls_enabled', 'tls_mode', 'tls_domain',
    'tls_cert_pem', 'tls_key_pem', 'mtls_enabled', 'setup_complete'
);
