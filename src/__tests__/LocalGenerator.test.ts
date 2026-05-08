import { describe, it, expect, vi, beforeEach } from 'vitest';
import { LocalConfigGeneratorService } from '../generators/LocalConfigGeneratorService';
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
    ],
    ...overrides,
  };
}

describe('LocalConfigGeneratorService', () => {
  const config = createConfig();
  let labelParser: LabelParserService;
  let serviceLocality: ServiceLocalityService;
  let generator: LocalConfigGeneratorService;

  beforeEach(() => {
    vi.clearAllMocks();
    labelParser = new LabelParserService();
    serviceLocality = new ServiceLocalityService(config);
    generator = new LocalConfigGeneratorService(
      serviceLocality,
      labelParser,
      config,
      mockLogger,
    );
  });

  describe('canGenerate', () => {
    it('should return true for local service', () => {
      const service = createService();
      expect(generator.canGenerate(service)).toBe(true);
    });

    it('should return false for remote service', () => {
      const service = createService({
        endpoints: [
          {
            nodeId: 'remote-node',
            nodeHostname: 'node2',
            nodeIp: '192.168.1.2',
            taskStatus: 'running',
            taskId: 'task-2',
          },
        ],
      });
      expect(generator.canGenerate(service)).toBe(false);
    });

    it('should return false for service with no endpoints', () => {
      const service = createService({ endpoints: [] });
      expect(generator.canGenerate(service)).toBe(false);
    });
  });

  describe('generate - local service', () => {
    it('should generate local config for local service', async () => {
      const service = createService();
      const result = await generator.generate(service);

      expect(result).not.toBeNull();
      expect(result!.http.services).toBeDefined();
      expect(result!.http.routers).toBeDefined();

      const localServiceName = 'test-app-local';
      expect(result!.http.services[localServiceName]).toBeDefined();
      expect(result!.http.routers[localServiceName]).toBeDefined();
    });

    it('should use Docker DNS internal address', async () => {
      const service = createService();
      const result = await generator.generate(service);

      const localServiceName = 'test-app-local';
      const servers =
        result!.http.services[localServiceName].loadBalancer.servers;
      expect(servers).toHaveLength(1);
      expect(servers[0].url).toBe('http://test-app:3000');
    });

    it('should generate router with federation header rule', async () => {
      const service = createService();
      const result = await generator.generate(service);

      const localServiceName = 'test-app-local';
      const router = result!.http.routers[localServiceName];

      expect(router.rule).toContain('Host(`test.local`)');
      expect(router.rule).toContain(
        'Headers(`X-Federated`, `true`)',
      );
      expect(router.service).toBe(localServiceName);
      expect(router.entryPoints).toEqual(['websecure']);
    });

    it('should include retry middleware when retryAttempts > 0', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.retryAttempts': '5',
        },
      });
      const result = await generator.generate(service);

      const localServiceName = 'test-app-local';
      const router = result!.http.routers[localServiceName];
      expect(router.middlewares).toContain('test-app-retry');
    });

    it('should not include retry middleware when retryAttempts is 0', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.retryAttempts': '0',
        },
      });
      const result = await generator.generate(service);

      const localServiceName = 'test-app-local';
      const router = result!.http.routers[localServiceName];
      expect(router.middlewares).toBeUndefined();
    });

    it('should include circuit breaker middleware when enabled', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.circuitBreaker': 'true',
        },
      });
      const result = await generator.generate(service);

      const localServiceName = 'test-app-local';
      const router = result!.http.routers[localServiceName];
      expect(router.middlewares).toContain('test-app-cb');
    });

    it('should include both retry and cb middlewares', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.retryAttempts': '5',
          'federation.circuitBreaker': 'true',
        },
      });
      const result = await generator.generate(service);

      const localServiceName = 'test-app-local';
      const router = result!.http.routers[localServiceName];
      expect(router.middlewares).toContain('test-app-retry');
      expect(router.middlewares).toContain('test-app-cb');
    });
  });

  describe('generate - health check', () => {
    it('should include health check in load balancer', async () => {
      const service = createService();
      const result = await generator.generate(service);

      const localServiceName = 'test-app-local';
      const lb =
        result!.http.services[localServiceName].loadBalancer;
      expect(lb.healthCheck).toBeDefined();
      expect(lb.healthCheck!.path).toBe('/');
      expect(lb.healthCheck!.interval).toBe('10s');
    });

    it('should use custom health check when configured', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.healthCheckPath': '/api/status',
          'federation.healthCheckInterval': '30s',
        },
      });
      const result = await generator.generate(service);

      const localServiceName = 'test-app-local';
      const hc =
        result!.http.services[localServiceName].loadBalancer.healthCheck;
      expect(hc!.path).toBe('/api/status');
      expect(hc!.interval).toBe('30s');
    });
  });

  describe('generate - remote service', () => {
    it('should return null for remote service', async () => {
      const service = createService({
        endpoints: [
          {
            nodeId: 'remote-node',
            nodeHostname: 'node2',
            nodeIp: '192.168.1.2',
            taskStatus: 'running',
            taskId: 'task-2',
          },
        ],
      });
      const result = await generator.generate(service);
      expect(result).toBeNull();
    });
  });

  describe('generate - invalid labels', () => {
    it('should return null for service without federation labels', async () => {
      const service = createService({
        labels: { 'some.other.label': 'true' },
      });
      const result = await generator.generate(service);
      expect(result).toBeNull();
    });
  });
});
