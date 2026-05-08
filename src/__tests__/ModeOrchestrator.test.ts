import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ModeOrchestratorService } from '../orchestration/ModeOrchestratorService.js';
import { GlobalOrchestratorService } from '../orchestration/GlobalOrchestratorService.js';
import { LocalOrchestratorService } from '../orchestration/LocalOrchestratorService.js';
import { ILogger } from '../core/interfaces/ILogger.js';

const mockLogger: ILogger = {
    info: vi.fn(),
    warn: vi.fn(),
    error: vi.fn(),
    debug: vi.fn(),
    fatal: vi.fn(),
    child: () => mockLogger,
};

describe('ModeOrchestratorService', () => {
    beforeEach(() => {
        vi.clearAllMocks();
    });

    describe('all mode', () => {
        it('should run both global and local orchestrators', async () => {
            const globalMock = {
                runGenerationCycle: vi.fn(),
            } as unknown as GlobalOrchestratorService;
            const localMock = {
                runGenerationCycle: vi.fn(),
            } as unknown as LocalOrchestratorService;

            const orchestrator = new ModeOrchestratorService(
                'all', mockLogger, globalMock, localMock,
            );
            await orchestrator.runGenerationCycle();

            expect(globalMock.runGenerationCycle).toHaveBeenCalledOnce();
            expect(localMock.runGenerationCycle).toHaveBeenCalledOnce();
        });
    });

    describe('global mode', () => {
        it('should run only global orchestrator', async () => {
            const globalMock = {
                runGenerationCycle: vi.fn(),
            } as unknown as GlobalOrchestratorService;
            const localMock = {
                runGenerationCycle: vi.fn(),
            } as unknown as LocalOrchestratorService;

            const orchestrator = new ModeOrchestratorService(
                'global', mockLogger, globalMock, localMock,
            );
            await orchestrator.runGenerationCycle();

            expect(globalMock.runGenerationCycle).toHaveBeenCalledOnce();
            expect(localMock.runGenerationCycle).not.toHaveBeenCalled();
        });

        it('should handle missing global orchestrator gracefully', async () => {
            const orchestrator = new ModeOrchestratorService('global', mockLogger);
            await expect(
                orchestrator.runGenerationCycle(),
            ).resolves.not.toThrow();
            expect(mockLogger.warn).toHaveBeenCalledWith(
                'Orquestrador global não configurado',
            );
        });

        it('should handle global generation errors without throwing', async () => {
            const globalMock = {
                runGenerationCycle: vi
                    .fn()
                    .mockRejectedValue(new Error('Failed')),
            } as unknown as GlobalOrchestratorService;

            const orchestrator = new ModeOrchestratorService(
                'global', mockLogger, globalMock,
            );
            await expect(
                orchestrator.runGenerationCycle(),
            ).resolves.not.toThrow();
            expect(mockLogger.error).toHaveBeenCalledWith(
                'Geração global falhou',
                expect.objectContaining({ error: 'Failed' }),
            );
        });
    });

    describe('local mode', () => {
        it('should run only local orchestrator', async () => {
            const globalMock = {
                runGenerationCycle: vi.fn(),
            } as unknown as GlobalOrchestratorService;
            const localMock = {
                runGenerationCycle: vi.fn(),
            } as unknown as LocalOrchestratorService;

            const orchestrator = new ModeOrchestratorService(
                'local', mockLogger, globalMock, localMock,
            );
            await orchestrator.runGenerationCycle();

            expect(globalMock.runGenerationCycle).not.toHaveBeenCalled();
            expect(localMock.runGenerationCycle).toHaveBeenCalledOnce();
        });

        it('should handle missing local orchestrator gracefully', async () => {
            const orchestrator = new ModeOrchestratorService('local', mockLogger);
            await expect(
                orchestrator.runGenerationCycle(),
            ).resolves.not.toThrow();
            expect(mockLogger.warn).toHaveBeenCalledWith(
                'Orquestrador local não configurado',
            );
        });

        it('should handle local generation errors without throwing', async () => {
            const localMock = {
                runGenerationCycle: vi
                    .fn()
                    .mockRejectedValue(new Error('Local failed')),
            } as unknown as LocalOrchestratorService;

            const orchestrator = new ModeOrchestratorService(
                'local', mockLogger, undefined, localMock,
            );
            await expect(
                orchestrator.runGenerationCycle(),
            ).resolves.not.toThrow();
            expect(mockLogger.error).toHaveBeenCalledWith(
                'Geração local falhou',
                expect.objectContaining({ error: 'Local failed' }),
            );
        });
    });

    describe('error handling', () => {
        it('should continue even if both orchestrators fail in all mode', async () => {
            const globalMock = {
                runGenerationCycle: vi
                    .fn()
                    .mockRejectedValue(new Error('Global error')),
            } as unknown as GlobalOrchestratorService;
            const localMock = {
                runGenerationCycle: vi
                    .fn()
                    .mockRejectedValue(new Error('Local error')),
            } as unknown as LocalOrchestratorService;

            const orchestrator = new ModeOrchestratorService(
                'all', mockLogger, globalMock, localMock,
            );
            await expect(
                orchestrator.runGenerationCycle(),
            ).resolves.not.toThrow();

            // Both errors should be logged
            const errorCalls = vi.mocked(mockLogger.error).mock.calls;
            const errorMessages = errorCalls.map((call) => call[0]);
            expect(errorMessages).toContain('Geração global falhou');
            expect(errorMessages).toContain('Geração local falhou');
        });
    });
});
