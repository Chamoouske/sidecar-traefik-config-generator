import { describe, it, expect } from 'vitest';
import { AppConfigService } from '../config/index';
import { ConfigValidationError } from '../types/errors';

describe('AppConfigService', () => {
  it('should load with defaults', () => {
    // On Windows, process.platform is 'win32' which causes the source
    // code to return a named pipe path instead of Unix socket.
    // Mock it to 'linux' to ensure consistent Unix path testing.
    const originalPlatform = Object.getOwnPropertyDescriptor(
      process, 'platform',
    );
    Object.defineProperty(process, 'platform', {
      value: 'linux',
    });
    try {
      const config = new AppConfigService({});
      const result = config.load();
      expect(result.docker.pollIntervalMs).toBe(30000);
      expect(result.docker.socket).toBe('/var/run/docker.sock');
      expect(result.server.port).toBe(9090);
      expect(result.logging.level).toBe('info');
      expect(result.logging.pretty).toBe(false);
      expect(result.federation.headerName).toBe('X-Federated');
      expect(result.federation.headerValue).toBe('true');
      expect(result.federation.circuitBreakerThreshold).toBe(0.3);
      expect(result.federation.defaultRetryAttempts).toBe(3);
      expect(result.federation.defaultRetryInterval).toBe('100ms');
      expect(result.node.hostname).toBeTruthy();
      expect(result.node.nodeId).toBe('unknown');
      expect(result.node.ip).toBe('127.0.0.1');
    } finally {
      Object.defineProperty(process, 'platform', originalPlatform!);
    }
  });

  it('should load from env vars', () => {
    const config = new AppConfigService({
      POLL_INTERVAL_MS: '10000',
      SERVER_PORT: '8080',
      LOG_LEVEL: 'debug',
    });
    const result = config.load();
    expect(result.docker.pollIntervalMs).toBe(10000);
    expect(result.server.port).toBe(8080);
    expect(result.logging.level).toBe('debug');
  });

  it('should load all env vars correctly', () => {
    const config = new AppConfigService({
      NODE_HOSTNAME: 'custom-node',
      NODE_IP: '10.0.0.50',
      NODE_ID: 'node-42',
      DOCKER_SOCKET: '//./pipe/custom_docker_engine',
      POLL_INTERVAL_MS: '5000',
      SHARED_DIR: '/custom/shared',
      LOCAL_DIR: '/custom/local',
      SERVER_PORT: '3000',
      HEALTH_ENDPOINT: '/status',
      LOG_LEVEL: 'warn',
      LOG_PRETTY: 'true',
      FEDERATION_HEADER_NAME: 'X-Custom-Fed',
      FEDERATION_HEADER_VALUE: 'enabled',
      CIRCUIT_BREAKER_THRESHOLD: '0.5',
      DEFAULT_RETRY_ATTEMPTS: '5',
      DEFAULT_RETRY_INTERVAL: '500ms',
    });
    const result = config.load();
    expect(result.node.hostname).toBe('custom-node');
    expect(result.node.ip).toBe('10.0.0.50');
    expect(result.node.nodeId).toBe('node-42');
    expect(result.docker.socket).toBe('//./pipe/custom_docker_engine');
    expect(result.docker.pollIntervalMs).toBe(5000);
    expect(result.directories.shared).toBe('/custom/shared');
    expect(result.directories.local).toBe('/custom/local');
    expect(result.directories.federation).toBe('/custom/shared/federation');
    expect(result.directories.middlewares).toBe('/custom/shared/middlewares');
    expect(result.directories.localGenerated).toBe('/custom/local/generated');
    expect(result.server.port).toBe(3000);
    expect(result.server.healthEndpoint).toBe('/status');
    expect(result.logging.level).toBe('warn');
    expect(result.logging.pretty).toBe(true);
    expect(result.federation.headerName).toBe('X-Custom-Fed');
    expect(result.federation.headerValue).toBe('enabled');
    expect(result.federation.circuitBreakerThreshold).toBe(0.5);
    expect(result.federation.defaultRetryAttempts).toBe(5);
    expect(result.federation.defaultRetryInterval).toBe('500ms');
  });

  it('should validate successfully with default config', () => {
    const config = new AppConfigService({});
    config.load();
    expect(() => config.validate()).not.toThrow();
  });

  it('should throw on invalid port (too large)', () => {
    const config = new AppConfigService({ SERVER_PORT: '99999' });
    config.load();
    expect(() => config.validate()).toThrow(ConfigValidationError);
    expect(() => config.validate()).toThrow(/Port/);
  });

  it('should throw on invalid port (zero)', () => {
    const config = new AppConfigService({ SERVER_PORT: '0' });
    config.load();
    expect(() => config.validate()).toThrow(ConfigValidationError);
  });

  it('should throw on invalid port (negative)', () => {
    const config = new AppConfigService({ SERVER_PORT: '-1' });
    config.load();
    expect(() => config.validate()).toThrow(ConfigValidationError);
  });

  it('should throw on poll interval too low', () => {
    const config = new AppConfigService({ POLL_INTERVAL_MS: '100' });
    config.load();
    expect(() => config.validate()).toThrow(ConfigValidationError);
    expect(() => config.validate()).toThrow(/Poll interval/);
  });

  it('should throw on shared and local directories being the same', () => {
    const config = new AppConfigService({
      SHARED_DIR: '/data/same',
      LOCAL_DIR: '/data/same',
    });
    config.load();
    expect(() => config.validate()).toThrow(ConfigValidationError);
    expect(() => config.validate()).toThrow(/different/);
  });

  it('should throw on invalid circuit breaker threshold (negative)', () => {
    const config = new AppConfigService({ CIRCUIT_BREAKER_THRESHOLD: '-0.1' });
    config.load();
    expect(() => config.validate()).toThrow(ConfigValidationError);
  });

  it('should throw on invalid circuit breaker threshold (> 1)', () => {
    const config = new AppConfigService({ CIRCUIT_BREAKER_THRESHOLD: '1.5' });
    config.load();
    expect(() => config.validate()).toThrow(ConfigValidationError);
  });

  it('should throw on negative retry attempts', () => {
    const config = new AppConfigService({ DEFAULT_RETRY_ATTEMPTS: '-1' });
    config.load();
    expect(() => config.validate()).toThrow(ConfigValidationError);
  });

  it('should accept zero retry attempts as valid', () => {
    const config = new AppConfigService({ DEFAULT_RETRY_ATTEMPTS: '0' });
    config.load();
    expect(() => config.validate()).not.toThrow();
    expect(config.get().federation.defaultRetryAttempts).toBe(0);
  });

  it('should return config via get() after load()', () => {
    const config = new AppConfigService({});
    config.load();
    const result = config.get();
    expect(result.server.port).toBe(9090);
  });

  it('get() should throw if load() was not called', () => {
    const config = new AppConfigService({});
    expect(() => config.get()).toThrow(/not loaded/);
  });

  it('should handle NaN poll interval by falling back to default', () => {
    const config = new AppConfigService({ POLL_INTERVAL_MS: 'not-a-number' });
    const result = config.load();
    expect(result.docker.pollIntervalMs).toBe(30000);
  });

  it('should handle NaN server port by falling back to default', () => {
    const config = new AppConfigService({ SERVER_PORT: 'not-a-number' });
    const result = config.load();
    expect(result.server.port).toBe(9090);
  });
});
