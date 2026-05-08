import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MiddlewareConfigGeneratorService } from '../generators/MiddlewareConfigGeneratorService';
import { LabelParserService } from '../services/LabelParserService';
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

describe('MiddlewareConfigGeneratorService', () => {
  let labelParser: LabelParserService;
  let generator: MiddlewareConfigGeneratorService;

  beforeEach(() => {
    vi.clearAllMocks();
    labelParser = new LabelParserService();
    generator = new MiddlewareConfigGeneratorService(
      labelParser,
      mockLogger,
    );
  });

  describe('generate', () => {
    it('should return null when service has no federation labels', async () => {
      const service = createService({
        labels: { 'some.other.label': 'true' },
      });
      const result = await generator.generate(service);
      expect(result).toBeNull();
    });

    it('should return null when service has no retry or circuit breaker configured', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.retryAttempts': '0',
        },
      });
      const result = await generator.generate(service);
      expect(result).toBeNull();
    });

    it('should generate retry middleware with default values', async () => {
      const service = createService();
      const result = await generator.generate(service);

      expect(result).not.toBeNull();
      expect(result!.http.middlewares).toBeDefined();
      expect(result!.http.middlewares['test-app-retry']).toBeDefined();
      expect(result!.http.middlewares['test-app-retry'].retry).toBeDefined();
      expect(result!.http.middlewares['test-app-retry'].retry!.attempts).toBe(3);
      expect(result!.http.middlewares['test-app-retry'].retry!.initialInterval).toBe('100ms');
    });

    it('should generate retry middleware with custom values', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.retryAttempts': '5',
          'federation.retryInterval': '500ms',
        },
      });
      const result = await generator.generate(service);

      expect(result).not.toBeNull();
      expect(result!.http.middlewares['test-app-retry']).toBeDefined();
      expect(result!.http.middlewares['test-app-retry'].retry!.attempts).toBe(5);
      expect(result!.http.middlewares['test-app-retry'].retry!.initialInterval).toBe('500ms');
    });

    it('should generate circuit breaker middleware', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.circuitBreaker': 'true',
          'federation.retryAttempts': '0',
        },
      });
      const result = await generator.generate(service);

      expect(result).not.toBeNull();
      expect(result!.http.middlewares['test-app-cb']).toBeDefined();
      expect(result!.http.middlewares['test-app-cb'].circuitBreaker).toBeDefined();
      expect(result!.http.middlewares['test-app-cb'].circuitBreaker!.expression).toContain(
        'NetworkErrorRatio()',
      );
    });

    it('should generate both retry and circuit breaker middlewares', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.retryAttempts': '5',
          'federation.retryInterval': '200ms',
          'federation.circuitBreaker': 'true',
        },
      });
      const result = await generator.generate(service);

      expect(result).not.toBeNull();

      expect(result!.http.middlewares['test-app-retry']).toBeDefined();
      expect(result!.http.middlewares['test-app-retry'].retry!.attempts).toBe(5);
      expect(result!.http.middlewares['test-app-retry'].retry!.initialInterval).toBe('200ms');

      expect(result!.http.middlewares['test-app-cb']).toBeDefined();
      expect(result!.http.middlewares['test-app-cb'].circuitBreaker!.expression).toContain(
        'NetworkErrorRatio()',
      );
    });

    it('should not include headers in middleware output', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'test.local',
          'federation.port': '3000',
          'federation.retryAttempts': '3',
        },
      });
      const result = await generator.generate(service);

      expect(result!.http.middlewares['test-app-retry'].headers).toBeUndefined();
    });

    it('should use service name as middleware key prefix', async () => {
      const service = createService({
        labels: {
          'federation.enable': 'true',
          'federation.host': 'my-custom-app.local',
          'federation.port': '8080',
          'federation.retryAttempts': '3',
          'federation.circuitBreaker': 'true',
        },
        serviceName: 'my-custom-app',
      });
      const result = await generator.generate(service);

      expect(result!.http.middlewares['my-custom-app-retry']).toBeDefined();
      expect(result!.http.middlewares['my-custom-app-cb']).toBeDefined();
    });
  });
});
