import { describe, it, expect, vi, beforeEach } from 'vitest';
import { FederationConfigGeneratorService } from '../generators/FederationConfigGeneratorService';
import { ServiceLocalityService } from '../services/ServiceLocalityService';
import { LabelParserService } from '../services/LabelParserService';
import { AppConfig } from '../types/config';
import { DiscoveredService } from '../types/docker';
import { ILogger } from '../core/interfaces/ILogger';

const mockLogger: ILogger = {
  info: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
  debug: vi.fn(),
  fatal: vi.fn(),
  child: () => mockLogger,
};

function createConfig(): AppConfig {
  return {
    mode: 'all',
    node: { hostname: 'node1', ip: '192.168.1.1', nodeId: 'local-node' },
    docker: { socket: '/var/run/docker.sock', pollIntervalMs: 30000 },
    directories: {
      shared: '/data/shared',
      local: '/data/local',
      federation: '/data/shared/federation',
      middlewares: '/data/shared/middlewares',
      localGenerated: '/data/local/generated',
    },
    federation: {
      headerName: 'X-Federated',
      headerValue: 'true',
      defaultHealthCheckPath: '/',
      defaultHealthCheckInterval: '10s',
      defaultRetryAttempts: 3,
      defaultRetryInterval: '100ms',
      circuitBreakerThreshold: 0.30,
    },
    server: { port: 9090, healthEndpoint: '/health' },
    logging: { level: 'info', pretty: false },
  };
}

function createService(
  overrides?: Partial<DiscoveredService> & { labels?: Record<string, string> },
): DiscoveredService {
  return {
    serviceName: 'test-app',
    serviceId: 'abc123',
    labels: {
      'federation.enable': 'true',
      'federation.host': 'test.local',
      'federation.port': '3000',
      ...overrides?.labels,
    },
    endpoints: [
      {
        nodeId: 'local-node',
        nodeHostname: 'node1',
        nodeIp: '192.168.1.1',
        taskStatus: 'running',
        taskId: 'task-1',
      },
      {
        nodeId: 'remote-node',
        nodeHostname: 'node2',
        nodeIp: '192.168.1.2',
        taskStatus: 'running',
        taskId: 'task-2',
      },
    ],
    ...overrides,
  };
}

describe('FederationConfigGeneratorService', () => {
  const config = createConfig();
  let labelParser: LabelParserService;
  let serviceLocality: ServiceLocalityService;
  let generator: FederationConfigGeneratorService;

  beforeEach(() => {
    vi.clearAllMocks();
    labelParser = new LabelParserService();
    serviceLocality = new ServiceLocalityService(config);
    generator = new FederationConfigGeneratorService(
      serviceLocality,
      labelParser,
      mockLogger,
    );
  });

  describe('canHandle', () => {
    it('should return true for federated service', () => {
      const service = createService();
      expect(generator.canHandle(service)).toBe(true);
    });

    it('should return false for non-federated service', () => {
      const service = createService({
        labels: { 'some.other.label': 'true' },
      });
      expect(generator.canHandle(service)).toBe(false);
    });

    it('should return false when required labels are missing', () => {
      const service = createService({
        labels: { 'federation.enable': 'true' },
      });
      expect(generator.canHandle(service)).toBe(false);
    });
  });

  describe('generate - basic', () => {
    it('should generate federation config with servers', async () => {
      const service = createService();
      const result = await generator.generate(service);

      expect(result.http.services).toBeDefined();
      expect(result.http.services['test-app']).toBeDefined();
      const serviceOutput = result.http.services['test-app'];

      expect(serviceOutput.loadBalancer.passHostHeader).toBe(true);
      expect(serviceOutput.loadBalancer.servers).toHaveLength(2);
    });

    it('should not generate circuit breaker by default', async () => {
      const service = createService();
      const result = await generator.generate(service);

      expect(
        result.http.services['test-app'].circuitBreaker,
      ).toBeUndefined();
    });

    it('should not generate sticky session by default', async () => {
      const service = createService();
      const result = await generator.generate(service);

      expect(
        result.http.services['test-app'].loadBalancer.sticky,
      ).toBeUndefined();
    });

    it('should generate health check by default when healthCheckPath exists', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
        },
      });
      const result = await generator.generate(service);

      expect(
        result.http.services['test-app'].loadBalancer.healthCheck,
      ).toBeDefined();
      expect(
        result.http.services['test-app'].loadBalancer.healthCheck!.path,
      ).toBe('/');
      expect(
        result.http.services['test-app'].loadBalancer.healthCheck!.interval,
      ).toBe('10s');
    });

    it('should generate empty services for invalid label config', async () => {
      const service = createService({
        labels: { 'some.label': 'true' },
      });
      const result = await generator.generate(service);

      expect(result.http.services).toEqual({});
    });
  });

  describe('generate - health check', () => {
    it('should use custom health check path and interval', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.healthCheckPath': '/api/health',
          'federation.healthCheckInterval': '15s',
        },
      });
      const result = await generator.generate(service);
      const hc =
        result.http.services['test-app'].loadBalancer.healthCheck;

      expect(hc).toBeDefined();
      expect(hc!.path).toBe('/api/health');
      expect(hc!.interval).toBe('15s');
    });
  });

  describe('generate - sticky session', () => {
    it('should generate sticky session when label is true', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.sticky': 'true',
        },
      });
      const result = await generator.generate(service);
      const sticky =
        result.http.services['test-app'].loadBalancer.sticky;

      expect(sticky).toBeDefined();
      expect(sticky!.cookie).toEqual({});
    });
  });

  describe('generate - circuit breaker', () => {
    it('should generate circuit breaker when label is true', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.circuitBreaker': 'true',
        },
      });
      const result = await generator.generate(service);
      const cb = result.http.services['test-app'].circuitBreaker;

      expect(cb).toBeDefined();
      expect(cb!.expression).toContain('NetworkErrorRatio()');
    });
  });

  describe('generate - combined (all options)', () => {
    it('should generate all options when all labels are set', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.sticky': 'true',
          'federation.circuitBreaker': 'true',
          'federation.healthCheckPath': '/api/health',
          'federation.healthCheckInterval': '15s',
          'federation.retryAttempts': '5',
          'federation.retryInterval': '200ms',
          'federation.localityAware': 'true',
        },
      });
      const result = await generator.generate(service);
      const svc = result.http.services['test-app'];

      expect(svc.loadBalancer.servers.length).toBeGreaterThan(0);
      expect(svc.loadBalancer.sticky).toBeDefined();
      expect(svc.loadBalancer.sticky!.cookie).toEqual({});

      expect(svc.loadBalancer.healthCheck).toBeDefined();
      expect(svc.loadBalancer.healthCheck!.path).toBe('/api/health');
      expect(svc.loadBalancer.healthCheck!.interval).toBe('15s');

      expect(svc.circuitBreaker).toBeDefined();
      expect(svc.circuitBreaker!.expression).toContain(
        'NetworkErrorRatio()',
      );
    });
  });

  describe('generate - weighted servers', () => {
    it('should use weighted servers from locality service', async () => {
      const service = createService();
      const result = await generator.generate(service);
      const servers = result.http.services['test-app'].loadBalancer.servers;

      const localServer = servers.find((s) =>
        s.url.includes('192.168.1.1'),
      );
      const remoteServer = servers.find((s) =>
        s.url.includes('192.168.1.2'),
      );

      expect(localServer).toBeDefined();
      expect(remoteServer).toBeDefined();
      expect(localServer!.weight).toBe(10);
      expect(remoteServer!.weight).toBe(1);
    });
  });
});
