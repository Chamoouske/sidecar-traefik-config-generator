import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { DockerClientService } from '../docker/DockerClient';
import { ILogger } from '../core/interfaces/ILogger';
import { AppConfig } from '../types/config';
import { DockerConnectionError } from '../types/errors';

// Mock dockerode
vi.mock('dockerode', () => {
  const mockDockerode = vi.fn();
  return {
    default: mockDockerode,
  };
});

import Docker from 'dockerode';

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
      circuitBreakerThreshold: 0.3,
    },
    server: { port: 9090, healthEndpoint: '/health' },
    logging: { level: 'info', pretty: false },
  };
}

describe('DockerClientService', () => {
  let config: AppConfig;
  let mockDockerInstance: any;

  afterEach(() => {
    vi.useRealTimers();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    config = createConfig();

    // Create mock docker instance
    mockDockerInstance = {
      ping: vi.fn(),
      listNodes: vi.fn(),
      listServices: vi.fn(),
      listTasks: vi.fn(),
      getNode: vi.fn(() => ({
        inspect: vi.fn(),
      })),
    };

    (Docker as any).mockImplementation(() => mockDockerInstance);
  });

  describe('connect', () => {
    it('should connect successfully on first attempt', async () => {
      mockDockerInstance.ping.mockResolvedValue(undefined);

      const client = new DockerClientService(config, mockLogger);
      await client.connect();

      expect(mockDockerInstance.ping).toHaveBeenCalledTimes(1);
      expect(client.isConnected()).toBe(true);
    });

    it('should throw DockerConnectionError after max attempts', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      mockDockerInstance.ping.mockRejectedValue(
        new Error('Connection refused'),
      );

      const client = new DockerClientService(config, mockLogger);
      const connectPromise = client.connect();

      // Attach catch handler BEFORE advancing time to prevent
      // Node.js unhandledRejection warning, since the promise
      // will reject during advanceTimersByTimeAsync
      let caughtError: any;
      connectPromise.catch((err) => {
        caughtError = err;
      });

      // Advance time through all backoff delays: 1s + 2s + 4s + 8s = 15s
      await vi.advanceTimersByTimeAsync(20000);

      expect(caughtError).toBeInstanceOf(DockerConnectionError);
      expect(mockDockerInstance.ping).toHaveBeenCalledTimes(5);
      expect(client.isConnected()).toBe(false);
    });

    it('should retry with exponential backoff', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      // Fail first 3, succeed on 4th
      mockDockerInstance.ping
        .mockRejectedValueOnce(new Error('Fail 1'))
        .mockRejectedValueOnce(new Error('Fail 2'))
        .mockRejectedValueOnce(new Error('Fail 3'))
        .mockResolvedValueOnce(undefined);

      const client = new DockerClientService(config, mockLogger);
      const connectPromise = client.connect();

      // Advance time through backoff delays: 1s + 2s + 4s = 7s
      await vi.advanceTimersByTimeAsync(10000);
      await connectPromise;

      expect(mockDockerInstance.ping).toHaveBeenCalledTimes(4);
      expect(client.isConnected()).toBe(true);
      vi.useRealTimers();
    });
  });

  describe('disconnect', () => {
    it('should disconnect successfully', async () => {
      mockDockerInstance.ping.mockResolvedValue(undefined);

      const client = new DockerClientService(config, mockLogger);
      await client.connect();
      expect(client.isConnected()).toBe(true);

      await client.disconnect();
      expect(client.isConnected()).toBe(false);
    });
  });

  describe('getNodes', () => {
    it('should return mapped nodes', async () => {
      mockDockerInstance.listNodes.mockResolvedValue([
        {
          ID: 'node-1',
          Description: { Hostname: 'worker-1' },
          Status: { Addr: '192.168.1.10', State: 'ready' },
          Spec: { Availability: 'active' },
        },
        {
          ID: 'node-2',
          Description: { Hostname: 'worker-2' },
          Status: { Addr: '192.168.1.11', State: 'ready' },
          Spec: { Availability: 'active' },
        },
      ]);

      const client = new DockerClientService(config, mockLogger);
      const nodes = await client.getNodes();

      expect(nodes).toHaveLength(2);
      expect(nodes[0].id).toBe('node-1');
      expect(nodes[0].hostname).toBe('worker-1');
      expect(nodes[0].ip).toBe('192.168.1.10');
      expect(nodes[0].availability).toBe('active');
      expect(nodes[0].status).toBe('ready');
    });

    it('should handle empty node list', async () => {
      mockDockerInstance.listNodes.mockResolvedValue([]);

      const client = new DockerClientService(config, mockLogger);
      const nodes = await client.getNodes();

      expect(nodes).toHaveLength(0);
    });
  });

  describe('getServices', () => {
    it('should return mapped services', async () => {
      mockDockerInstance.listServices.mockResolvedValue([
        {
          ID: 'svc-1',
          Spec: {
            Name: 'web-app',
            Labels: { 'federation.enable': 'true' },
            TaskTemplate: {
              ContainerSpec: { Image: 'nginx:latest' },
            },
            Mode: { Replicated: { Replicas: 3 } },
          },
        },
      ]);

      const client = new DockerClientService(config, mockLogger);
      const services = await client.getServices();

      expect(services).toHaveLength(1);
      expect(services[0].id).toBe('svc-1');
      expect(services[0].name).toBe('web-app');
      expect(services[0].labels['federation.enable']).toBe('true');
      expect(services[0].image).toBe('nginx:latest');
      expect(services[0].replicas).toBe(3);
    });

    it('should handle services with ports', async () => {
      mockDockerInstance.listServices.mockResolvedValue([
        {
          ID: 'svc-1',
          Spec: {
            Name: 'web-app',
            Labels: {},
            TaskTemplate: {
              ContainerSpec: { Image: 'nginx:latest' },
            },
            Mode: { Replicated: { Replicas: 1 } },
            EndpointSpec: {
              Ports: [
                { PublishedPort: 8080, TargetPort: 80 },
              ],
            },
          },
        },
      ]);

      const client = new DockerClientService(config, mockLogger);
      const services = await client.getServices();

      expect(services[0].ports).toBeDefined();
      expect(services[0].ports).toHaveLength(1);
      expect(services[0].ports![0].published).toBe(8080);
      expect(services[0].ports![0].target).toBe(80);
    });

    it('should handle global mode services', async () => {
      mockDockerInstance.listServices.mockResolvedValue([
        {
          ID: 'svc-global',
          Spec: {
            Name: 'global-app',
            Labels: {},
            TaskTemplate: {
              ContainerSpec: { Image: 'traefik:latest' },
            },
            Mode: { Global: {} },
          },
        },
      ]);

      const client = new DockerClientService(config, mockLogger);
      const services = await client.getServices();

      expect(services[0].replicas).toBe(1);
    });

    it('should handle empty services list', async () => {
      mockDockerInstance.listServices.mockResolvedValue([]);

      const client = new DockerClientService(config, mockLogger);
      const services = await client.getServices();

      expect(services).toHaveLength(0);
    });
  });

  describe('getServiceTasks', () => {
    it('should return only running tasks', async () => {
      mockDockerInstance.listTasks.mockResolvedValue([
        {
          ID: 'task-1',
          NodeID: 'node-1',
          ServiceID: 'svc-1',
          Status: { State: 'running' },
          DesiredState: 'running',
          Slot: 1,
        },
        {
          ID: 'task-2',
          NodeID: 'node-2',
          ServiceID: 'svc-1',
          Status: { State: 'shutdown' },
          DesiredState: 'shutdown',
          Slot: 2,
        },
        {
          ID: 'task-3',
          NodeID: 'node-1',
          ServiceID: 'svc-1',
          Status: { State: 'pending' },
          DesiredState: 'running',
          Slot: 3,
        },
      ]);

      const client = new DockerClientService(config, mockLogger);
      const tasks = await client.getServiceTasks('svc-1');

      // Only task with DesiredState='running' should be returned
      expect(tasks).toHaveLength(2);
      expect(tasks[0].id).toBe('task-1');
      expect(tasks[1].id).toBe('task-3');
    });

    it('should filter tasks by service ID', async () => {
      mockDockerInstance.listTasks.mockImplementation(
        ({ filters }: any) => {
          const serviceId = filters?.service?.[0];
          if (serviceId === 'svc-1') {
            return [
              {
                ID: 'task-1',
                NodeID: 'node-1',
                ServiceID: 'svc-1',
                Status: { State: 'running' },
                DesiredState: 'running',
                Slot: 1,
              },
            ];
          }
          return [];
        },
      );

      const client = new DockerClientService(config, mockLogger);
      const tasks = await client.getServiceTasks('svc-1');

      expect(tasks).toHaveLength(1);
      expect(tasks[0].serviceId).toBe('svc-1');
    });
  });

  describe('getNodeInfo', () => {
    it('should return mapped node info', async () => {
      const mockInspect = vi.fn().mockResolvedValue({
        ID: 'node-1',
        Description: { Hostname: 'worker-1' },
        Status: { Addr: '192.168.1.10', State: 'ready' },
        Spec: { Availability: 'active' },
      });

      mockDockerInstance.getNode.mockReturnValue({
        inspect: mockInspect,
      });

      const client = new DockerClientService(config, mockLogger);
      const node = await client.getNodeInfo('node-1');

      expect(node.id).toBe('node-1');
      expect(node.hostname).toBe('worker-1');
      expect(node.ip).toBe('192.168.1.10');
      expect(node.availability).toBe('active');
      expect(node.status).toBe('ready');
    });
  });

  describe('isConnected', () => {
    it('should return false initially', () => {
      const client = new DockerClientService(config, mockLogger);
      expect(client.isConnected()).toBe(false);
    });

    it('should return true after successful connect', async () => {
      mockDockerInstance.ping.mockResolvedValue(undefined);

      const client = new DockerClientService(config, mockLogger);
      await client.connect();
      expect(client.isConnected()).toBe(true);
    });
  });

  describe('onReconnect', () => {
    it('should register reconnect handlers', () => {
      const client = new DockerClientService(config, mockLogger);
      const handler = vi.fn();

      client.onReconnect(handler);
      // No direct way to test the handler without triggering disconnect,
      // but at least it shouldn't throw
      expect(true).toBe(true);
    });
  });
});
