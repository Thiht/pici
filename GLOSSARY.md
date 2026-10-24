# Pici

Pici runs CI workflows for git repositories and reports their results.

## Executions

**Execution**:
A logical run of one workflow on one project, identified by its first attempt. It groups every attempt made to complete that run.
_Avoid_: Run, build, job

**Attempt**:
One concrete try at completing an Execution. The first attempt starts the Execution; a retry adds another attempt to the same Execution.
_Avoid_: Run, try, execution

**Retry**:
The act of adding a new attempt to a failed Execution. A retry continues the same Execution instead of starting a new one.
_Avoid_: Rerun, rebuild

**Rebuild**:
A new Execution started from the same workflow and commit as an earlier Execution. A rebuild is a new Execution, not a new attempt.
_Avoid_: Retry, rerun

## Workflows

**Step**:
A unit of work declared in a workflow's `ci.yml`, executed inside the workflow image.
_Avoid_: Job, task

**Step retry**:
An automatic re-execution of a failed step within the same attempt, configured by `retry` on the step. It is not an Execution attempt.
_Avoid_: Attempt

## Artifacts

**Artifact**:
A file produced by a Step and kept with the Execution so it can be downloaded after the run.
_Avoid_: Output, attachment, build asset

**Artifact hash**:
The digest of an Artifact's exact bytes, recorded when the Artifact is collected.
_Avoid_: Checksum, fingerprint, signature
