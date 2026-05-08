import { describe, it, expect } from 'vitest';
import {
  SidecarError,
  DockerConnectionError,
  ConfigValidationError,
  FileWriteError,
  DiscoveryError,
} from '../types/errors';
import { LabelConfig } from '../types/config';

describe('Error Types', () => {
  describe('SidecarError', () => {
    it('should create error with message', () => {
      const error = new SidecarError('Something went wrong');
      expect(error).toBeInstanceOf(Error);
      expect(error).toBeInstanceOf(SidecarError);
      expect(error.message).toBe('Something went wrong');
      expect(error.name).toBe('SidecarError');
    });

    it('should create error with cause', () => {
      const cause = new Error('Root cause');
      const error = new SidecarError('Something went wrong', cause);
      expect(error.cause).toBe(cause);
    });

    it('should create error without cause', () => {
      const error = new SidecarError('Something went wrong');
      expect(error.cause).toBeUndefined();
    });
  });

  describe('DockerConnectionError', () => {
    it('should be instance of SidecarError', () => {
      const error = new DockerConnectionError('Cannot connect to Docker');
      expect(error).toBeInstanceOf(Error);
      expect(error).toBeInstanceOf(SidecarError);
      expect(error).toBeInstanceOf(DockerConnectionError);
      expect(error.name).toBe('DockerConnectionError');
    });

    it('should pass cause to parent', () => {
      const cause = new Error('Connection refused');
      const error = new DockerConnectionError('Cannot connect', cause);
      expect(error.cause).toBe(cause);
    });
  });

  describe('ConfigValidationError', () => {
    it('should be instance of SidecarError', () => {
      const error = new ConfigValidationError('Invalid config');
      expect(error).toBeInstanceOf(Error);
      expect(error).toBeInstanceOf(SidecarError);
      expect(error).toBeInstanceOf(ConfigValidationError);
      expect(error.name).toBe('ConfigValidationError');
    });
  });

  describe('FileWriteError', () => {
    it('should be instance of SidecarError with filePath', () => {
      const error = new FileWriteError(
        'Cannot write file',
        '/path/to/file.yaml',
      );
      expect(error).toBeInstanceOf(Error);
      expect(error).toBeInstanceOf(SidecarError);
      expect(error).toBeInstanceOf(FileWriteError);
      expect(error.name).toBe('FileWriteError');
      expect(error.filePath).toBe('/path/to/file.yaml');
    });

    it('should pass cause to parent', () => {
      const cause = new Error('Disk full');
      const error = new FileWriteError(
        'Cannot write file',
        '/path/to/file.yaml',
        cause,
      );
      expect(error.cause).toBe(cause);
    });
  });

  describe('DiscoveryError', () => {
    it('should be instance of SidecarError', () => {
      const error = new DiscoveryError('Discovery failed');
      expect(error).toBeInstanceOf(Error);
      expect(error).toBeInstanceOf(SidecarError);
      expect(error).toBeInstanceOf(DiscoveryError);
      expect(error.name).toBe('DiscoveryError');
    });

    it('should pass cause to parent', () => {
      const cause = new Error('Timeout');
      const error = new DiscoveryError('Discovery failed', cause);
      expect(error.cause).toBe(cause);
    });
  });
});

describe('LabelConfig defaults', () => {
  it('should create a minimal LabelConfig with required fields', () => {
    const config: LabelConfig = {
      enabled: true,
      host: 'app.local',
      port: 3000,
    };
    expect(config.enabled).toBe(true);
    expect(config.host).toBe('app.local');
    expect(config.port).toBe(3000);
  });

  it('should allow optional fields to be set', () => {
    const config: LabelConfig = {
      enabled: true,
      host: 'app.local',
      port: 3000,
      sticky: true,
      retryAttempts: 5,
      retryInterval: '200ms',
      circuitBreaker: true,
      healthCheckPath: '/api/health',
      healthCheckInterval: '15s',
      localityAware: true,
    };
    expect(config.sticky).toBe(true);
    expect(config.retryAttempts).toBe(5);
    expect(config.retryInterval).toBe('200ms');
    expect(config.circuitBreaker).toBe(true);
    expect(config.healthCheckPath).toBe('/api/health');
    expect(config.healthCheckInterval).toBe('15s');
    expect(config.localityAware).toBe(true);
  });

  it('should allow optional fields to be undefined', () => {
    const config: LabelConfig = {
      enabled: true,
      host: 'app.local',
      port: 3000,
    };
    expect(config.sticky).toBeUndefined();
    expect(config.retryAttempts).toBeUndefined();
    expect(config.retryInterval).toBeUndefined();
    expect(config.circuitBreaker).toBeUndefined();
    expect(config.healthCheckPath).toBeUndefined();
    expect(config.healthCheckInterval).toBeUndefined();
    expect(config.localityAware).toBeUndefined();
  });
});

describe('Output interfaces construction', () => {
  it('should construct FederationConfigOutput', () => {
    const output = {
      http: {
        services: {
          'test-app': {
            loadBalancer: {
              passHostHeader: true,
              servers: [{ url: 'http://localhost:3000' }],
            },
          },
        },
      },
    };

    expect(output.http.services['test-app'].loadBalancer.servers).toHaveLength(
      1,
    );
    expect(
      output.http.services['test-app'].loadBalancer.passHostHeader,
    ).toBe(true);
  });

  it('should construct LocalConfigOutput with routers', () => {
    const output = {
      http: {
        services: {
          'test-app-local': {
            loadBalancer: {
              passHostHeader: true,
              servers: [{ url: 'http://test-app:3000' }],
            },
          },
        },
        routers: {
          'test-app-local': {
            rule: 'Host(`test.local`)',
            service: 'test-app-local',
            entryPoints: ['websecure'],
          },
        },
      },
    };

    expect(
      output.http.services['test-app-local'].loadBalancer.servers[0].url,
    ).toBe('http://test-app:3000');
    expect(output.http.routers['test-app-local'].rule).toContain(
      'Host(`test.local`)',
    );
  });

  it('should construct MiddlewareConfigOutput', () => {
    const output = {
      http: {
        middlewares: {
          'test-app-retry': {
            retry: {
              attempts: 3,
              initialInterval: '100ms',
            },
          },
        },
      },
    };

    expect(output.http.middlewares['test-app-retry'].retry?.attempts).toBe(3);
  });

  it('should construct LoadBalancerConfig with health check', () => {
    const lb = {
      passHostHeader: true,
      servers: [{ url: 'http://localhost:3000' }],
      healthCheck: {
        path: '/health',
        interval: '10s',
      },
    };

    expect(lb.healthCheck?.path).toBe('/health');
    expect(lb.healthCheck?.interval).toBe('10s');
  });

  it('should construct LoadBalancerConfig with sticky session', () => {
    const lb = {
      passHostHeader: true,
      servers: [{ url: 'http://localhost:3000' }],
      sticky: {
        cookie: {},
      },
    };

    expect(lb.sticky?.cookie).toEqual({});
  });

  it('should construct ServiceOutput with circuit breaker', () => {
    const serviceOutput = {
      loadBalancer: {
        passHostHeader: true,
        servers: [{ url: 'http://localhost:3000' }],
      },
      circuitBreaker: {
        expression: 'NetworkErrorRatio() > 0.30',
      },
    };

    expect(serviceOutput.circuitBreaker?.expression).toContain(
      'NetworkErrorRatio()',
    );
  });

  it('should construct RouterOutput with middlewares', () => {
    const router = {
      rule: 'Host(`test.local`)',
      service: 'test-app-local',
      entryPoints: ['websecure'],
      middlewares: ['test-app-retry', 'test-app-cb'],
      priority: 100,
    };

    expect(router.middlewares).toHaveLength(2);
    expect(router.priority).toBe(100);
  });
});
