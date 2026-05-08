/**
 * Interface for local orchestrator.
 * Generates node-specific local route configurations.
 */
export interface ILocalOrchestrator {
    runGenerationCycle(): Promise<void>;
}
