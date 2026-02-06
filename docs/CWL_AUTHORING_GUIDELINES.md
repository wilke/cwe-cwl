# CWL Authoring Guidelines for BV-BRC

This document provides guidelines for writing CWL (Common Workflow Language) tools and workflows that are compatible with the BV-BRC CWL Workflow Engine.

## Executable Requirements

### Use $PATH-relative Commands

**DO NOT** use relative or absolute paths to executables. Always assume the executable is available in `$PATH`.

```yaml
# CORRECT - executable in $PATH
baseCommand: boltz

# CORRECT - command with subcommand
baseCommand: [boltz, predict]

# INCORRECT - relative path
baseCommand: ./bin/boltz

# INCORRECT - absolute path
baseCommand: /opt/tools/boltz/bin/boltz
```

The execution engine will verify executables exist using `which <executable>` before running.

### Container Requirements (REQUIRED)

All CWL tools **MUST** specify a container requirement. The engine uses containers for reproducibility and isolation.

#### DockerRequirement (Recommended)

```yaml
requirements:
  DockerRequirement:
    dockerPull: biocontainers/bwa:0.7.17--h7132678_9
```

The engine supports multiple container runtimes and will translate `DockerRequirement` appropriately:

| Runtime | Image Format | Notes |
|---------|--------------|-------|
| Docker | `image:tag` | Native Docker pull |
| Podman | `image:tag` | Docker-compatible |
| Apptainer/Singularity | `docker://image:tag` | Auto-converted from dockerPull |

#### Image Naming Conventions

Use fully-qualified image names from trusted registries:

```yaml
# BioContainers (recommended for bioinformatics)
dockerPull: biocontainers/samtools:1.17--h00cdaf9_0

# Quay.io
dockerPull: quay.io/biocontainers/bwa:0.7.17--h7132678_9

# Docker Hub (official images)
dockerPull: python:3.11-slim

# Custom BV-BRC images
dockerPull: ghcr.io/bv-brc/boltz-bvbrc:1.0.0

# GPU-enabled images (tag convention)
dockerPull: dxkb/boltz-bvbrc:latest-gpu
```

#### Apptainer/Singularity Native Images

For workflows that require native Apptainer/Singularity images (`.sif` files), use the custom `ApptainerRequirement` hint:

```yaml
hints:
  ApptainerRequirement:
    # Pull from library
    apptainerPull: library://sylabs/examples/lolcow:latest

    # Or reference a local .sif file
    apptainerFile: /path/to/container.sif

    # Or build from definition file
    apptainerBuild: container.def
```

**Note:** `ApptainerRequirement` is a BV-BRC extension. For maximum portability, prefer `DockerRequirement`.

#### Container Priority

When both are specified, the engine uses this priority:
1. `ApptainerRequirement` (if Apptainer runtime configured)
2. `DockerRequirement` (default)

## Resource Requirements

Always specify resource requirements for proper scheduling:

```yaml
requirements:
  ResourceRequirement:
    coresMin: 4          # Minimum CPU cores
    coresMax: 16         # Maximum (optional)
    ramMin: 8192         # Minimum RAM in MB (8 GB)
    ramMax: 65536        # Maximum RAM in MB (optional)
    tmpdirMin: 10240     # Temp directory space in MB
    outdirMin: 10240     # Output directory space in MB
```

### GPU Requirements

For GPU-enabled tools, use the cwltool extension:

```yaml
hints:
  cwltool:CUDARequirement:
    cudaVersionMin: "11.0"
    cudaComputeCapability: "7.0"
    cudaDeviceCountMin: 1
    cudaDeviceCountMax: 4
```

## Input/Output Best Practices

### Input Bindings

```yaml
inputs:
  input_file:
    type: File
    inputBinding:
      position: 1
    doc: "Input FASTA file with sequences"

  output_prefix:
    type: string
    default: "output"
    inputBinding:
      prefix: --prefix
    doc: "Prefix for output files"

  num_threads:
    type: int?
    default: 4
    inputBinding:
      prefix: -t
    doc: "Number of threads"
```

### Output Bindings

Use glob patterns to capture outputs:

```yaml
outputs:
  aligned_file:
    type: File
    outputBinding:
      glob: "*.bam"
    doc: "Aligned reads in BAM format"

  all_outputs:
    type: Directory
    outputBinding:
      glob: $(inputs.output_prefix)
    doc: "All output files"
```

### Secondary Files

Specify secondary files (e.g., index files):

```yaml
inputs:
  bam_file:
    type: File
    secondaryFiles:
      - .bai
      - ^.bai  # Alternative naming: file.bai instead of file.bam.bai
```

## Workflow Best Practices

### Step Dependencies

Dependencies are inferred from input sources:

```yaml
steps:
  align:
    run: bwa.cwl
    in:
      reads: input_reads
      reference: reference_genome
    out: [aligned]

  sort:
    run: samtools_sort.cwl
    in:
      input_bam: align/aligned  # Creates dependency on 'align' step
    out: [sorted]
```

### Parallel Execution

Steps without dependencies run in parallel:

```yaml
steps:
  # These run in parallel (no shared dependencies)
  qc_reads:
    run: fastqc.cwl
    in:
      reads: input_reads
    out: [report]

  index_reference:
    run: bwa_index.cwl
    in:
      reference: reference_genome
    out: [indexed]

  # This waits for both above to complete
  align:
    run: bwa_mem.cwl
    in:
      reads: input_reads
      reference: index_reference/indexed
    out: [aligned]
```

### Scatter (Parallel Processing)

Process arrays in parallel:

```yaml
requirements:
  ScatterFeatureRequirement: {}

steps:
  process_samples:
    run: process.cwl
    scatter: sample
    scatterMethod: dotproduct  # or flat_crossproduct
    in:
      sample: samples_array
    out: [result]
```

## Complete Tool Example

```yaml
#!/usr/bin/env cwl-runner
cwlVersion: v1.2
class: CommandLineTool

label: "BWA MEM Alignment"
doc: |
  Aligns reads to a reference genome using BWA MEM algorithm.
  Outputs aligned reads in BAM format.

requirements:
  DockerRequirement:
    dockerPull: biocontainers/bwa:0.7.17--h7132678_9
  ResourceRequirement:
    coresMin: 4
    ramMin: 8192
    tmpdirMin: 10240
  InlineJavascriptRequirement: {}

baseCommand: [bwa, mem]

arguments:
  - position: 100
    prefix: -t
    valueFrom: $(runtime.cores)

inputs:
  reference:
    type: File
    secondaryFiles:
      - .amb
      - .ann
      - .bwt
      - .pac
      - .sa
    inputBinding:
      position: 1
    doc: "Reference genome (indexed)"

  reads:
    type: File[]
    inputBinding:
      position: 2
    doc: "Input reads (FASTQ)"

  output_name:
    type: string
    default: "aligned.sam"
    doc: "Output filename"

stdout: $(inputs.output_name)

outputs:
  aligned:
    type: stdout
    doc: "Aligned reads in SAM format"
```

## Complete Workflow Example

```yaml
#!/usr/bin/env cwl-runner
cwlVersion: v1.2
class: Workflow

label: "Read Alignment Pipeline"
doc: |
  Aligns reads to reference and sorts the output.

requirements:
  SubworkflowFeatureRequirement: {}
  ScatterFeatureRequirement: {}
  InlineJavascriptRequirement: {}

inputs:
  reads:
    type: File[]
    doc: "Input FASTQ files"

  reference:
    type: File
    secondaryFiles: [.amb, .ann, .bwt, .pac, .sa]
    doc: "BWA-indexed reference genome"

steps:
  align:
    run: bwa_mem.cwl
    in:
      reference: reference
      reads: reads
    out: [aligned]

  sort:
    run: samtools_sort.cwl
    in:
      input_sam: align/aligned
    out: [sorted_bam]

  index:
    run: samtools_index.cwl
    in:
      input_bam: sort/sorted_bam
    out: [indexed_bam]

outputs:
  aligned_bam:
    type: File
    secondaryFiles: [.bai]
    outputSource: index/indexed_bam
    doc: "Sorted and indexed BAM file"
```

## Validation

Before submitting, validate your CWL:

```bash
# Using cwe-cli
cwe-cli validate my_tool.cwl

# Using cwltool
cwltool --validate my_tool.cwl
```

## References

- [CWL v1.2 Specification](https://www.commonwl.org/v1.2/)
- [CWL User Guide](https://www.commonwl.org/user_guide/)
- [BioContainers Registry](https://biocontainers.pro/)
- [cwltool Documentation](https://cwltool.readthedocs.io/)
