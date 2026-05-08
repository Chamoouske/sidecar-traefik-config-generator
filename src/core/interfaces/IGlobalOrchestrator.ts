/**
 * Interface for global orchestrator.
 * Generates shared federation and middleware configurations.
 */
export interface IGlobalOrchestrator {
    runGenerationCycle(): Promise<void>;
}
