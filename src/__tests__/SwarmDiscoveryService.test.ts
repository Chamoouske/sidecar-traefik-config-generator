import { describe, it, expect, vi, beforeEach } from 'vitest';
import { SwarmDiscoveryService } from '../services/SwarmDiscoveryService';
import { IDockerClient } from '../core/interfaces/IDockerClient';
import { ILabelParser } from '../core/interfaces/ILabelParser';
import { ILogger } from '../core/interfaces/ILogger';
import { LabelParserService } from '../services/LabelParserService';

const mockLogger: ILogger = {
  info: vi.fn(),
  warn: vi.fn(),
  error: vi.fn(),
  debug: vi.fn(),
  fatal: vi.fn(),
  child: () => mockLogger,
};

function createMockDockerClient(): IDockerClient {
  return {
    connect: vi.fn(),
    disconnect: vi.fn(),
    getNodes: vi.fn(),
    getServices: vi.fn() as any,
    getServiceTasks: vi.fn() as any,
    getNodeInfo: vi.fn() as any,
    isConnected: vi.fn(),
    onReconnect: vi.fn(),
  };
}

describe('SwarmDiscoveryService', () => {
  let dockerClient: IDockerClient;
  let labelParser: ILabelParser;
  let discovery: SwarmDiscoveryService;

  beforeEach(() => {
    vi.clearAllMocks();
    dockerClient = createMockDockerClient();
    labelParser = new LabelParserService();
    discovery = new SwarmDiscoveryService(
      dockerClient,
      labelParser,
      mockLogger,
    );
  });

  describe('discoverAllServices', () => {
    it('should discover federated services with endpoints', async () => {
      dockerClient.getServices.mockResolvedValue([
        {
          id: 'svc-1',
          name: 'app-1',
          labels: { 'federation.enable': 'true' },
          image: 'nginx:latest',
          replicas: 2,
        },
        {
          id: 'svc-2',
          name: 'app-2',
          labels: { 'federation.enable': 'false' },
          image: 'redis:latest',
          replicas: 1,
        },
      ]);

      dockerClient.getServiceTasks.mockResolvedValue([
        {
          id: 'task-1',
          nodeId: 'node-1',
          serviceId: 'svc-1',
          status: 'running',
          desiredState: 'running',
          slot: 1,
        },
      ]);

      dockerClient.getNodeInfo.mockResolvedValue({
        id: 'node-1',
        hostname: 'worker-1',
        ip: '192.168.1.10',
        availability: 'active',
        status: 'ready',
      });

      const result = await discovery.discoverAllServices();

      expect(result).toHaveLength(1);
      expect(result[0].serviceName).toBe('app-1');
      expect(result[0].endpoints).toHaveLength(1);
      expect(result[0].endpoints[0].nodeIp).toBe('192.168.1.10');
    });

    it('should return empty array when no federated services exist', async () => {
      dockerClient.getServices.mockResolvedValue([
        {
          id: 'svc-1',
          name: 'app-1',
          labels: {},
          image: 'nginx:latest',
          replicas: 1,
        },
      ]);

      const result = await discovery.discoverAllServices();
      expect(result).toHaveLength(0);
    });

    it('should return empty array when no services exist', async () => {
      dockerClient.getServices.mockResolvedValue([]);

      const result = await discovery.discoverAllServices();
      expect(result).toHaveLength(0);
    });

    it('should handle node info errors gracefully', async () => {
      dockerClient.getServices.mockResolvedValue([
        {
          id: 'svc-1',
          name: 'app-1',
          labels: { 'federation.enable': 'true' },
          image: 'nginx:latest',
          replicas: 2,
        },
      ]);

      dockerClient.getServiceTasks.mockResolvedValue([
        {
          id: 'task-1',
          nodeId: 'node-1',
          serviceId: 'svc-1',
          status: 'running',
          desiredState: 'running',
          slot: 1,
        },
      ]);

      dockerClient.getNodeInfo.mockRejectedValue(
        new Error('Node not found'),
      );

      const result = await discovery.discoverAllServices();

      // Service should still be discovered, but with no endpoints
      expect(result).toHaveLength(1);
      expect(result[0].endpoints).toHaveLength(0);
      expect(mockLogger.warn).toHaveBeenCalled();
    });

    it('should process multiple tasks for a single service', async () => {
      dockerClient.getServices.mockResolvedValue([
        {
          id: 'svc-1',
          name: 'app-1',
          labels: { 'federation.enable': 'true' },
          image: 'nginx:latest',
          replicas: 3,
        },
      ]);

      dockerClient.getServiceTasks.mockResolvedValue([
        {
          id: 'task-1',
          nodeId: 'node-1',
          serviceId: 'svc-1',
          status: 'running',
          desiredState: 'running',
          slot: 1,
        },
        {
          id: 'task-2',
          nodeId: 'node-2',
          serviceId: 'svc-1',
          status: 'running',
          desiredState: 'running',
          slot: 2,
        },
      ]);

      dockerClient.getNodeInfo
        .mockResolvedValueOnce({
          id: 'node-1',
          hostname: 'worker-1',
          ip: '192.168.1.10',
          availability: 'active',
          status: 'ready',
        })
        .mockResolvedValueOnce({
          id: 'node-2',
          hostname: 'worker-2',
          ip: '192.168.1.11',
          availability: 'active',
          status: 'ready',
        });

      const result = await discovery.discoverAllServices();

      expect(result).toHaveLength(1);
      expect(result[0].endpoints).toHaveLength(2);
    });
  });

  describe('discoverLocalServices', () => {
    it('should return only services with local endpoints', async () => {
      dockerClient.getServices.mockResolvedValue([
        {
          id: 'svc-local',
          name: 'app-local',
          labels: { 'federation.enable': 'true' },
          image: 'nginx:latest',
          replicas: 2,
        },
        {
          id: 'svc-remote',
          name: 'app-remote',
          labels: { 'federation.enable': 'true' },
          image: 'redis:latest',
          replicas: 1,
        },
      ]);

      dockerClient.getServiceTasks
        .mockResolvedValueOnce([
          {
            id: 'task-local',
            nodeId: 'local-node',
            serviceId: 'svc-local',
            status: 'running',
            desiredState: 'running',
            slot: 1,
          },
        ])
        .mockResolvedValueOnce([
          {
            id: 'task-remote',
            nodeId: 'remote-node',
            serviceId: 'svc-remote',
            status: 'running',
            desiredState: 'running',
            slot: 1,
          },
        ]);

      dockerClient.getNodeInfo
        .mockResolvedValueOnce({
          id: 'local-node',
          hostname: 'local-node',
          ip: '127.0.0.1',
          availability: 'active',
          status: 'ready',
        })
        .mockResolvedValueOnce({
          id: 'remote-node',
          hostname: 'remote-node',
          ip: '192.168.1.100',
          availability: 'active',
          status: 'ready',
        });

      // Set NODE_ID for the test
      const originalNodeId = process.env.NODE_ID;
      process.env.NODE_ID = 'local-node';

      const result = await discovery.discoverLocalServices();

      expect(result).toHaveLength(1);
      expect(result[0].serviceName).toBe('app-local');

      // Restore
      process.env.NODE_ID = originalNodeId;
    });
  });

  describe('getCurrentNodeId', () => {
    it('should return NODE_ID from environment', () => {
      const originalNodeId = process.env.NODE_ID;
      process.env.NODE_ID = 'my-node';

      const result = discovery.getCurrentNodeId();
      expect(result).toBe('my-node');

      process.env.NODE_ID = originalNodeId;
    });

    it('should return "unknown" when NODE_ID is not set', () => {
      const originalNodeId = process.env.NODE_ID;
      delete process.env.NODE_ID;

      const result = discovery.getCurrentNodeId();
      expect(result).toBe('unknown');

      process.env.NODE_ID = originalNodeId;
    });
  });
});
