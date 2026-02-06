#!/usr/bin/env cwl-runner
cwlVersion: v1.2
class: CommandLineTool

label: AlphaFold Structure Prediction
doc: |
  Predicts protein 3D structure from amino acid sequence using AlphaFold.
  Requires GPU for efficient execution.

requirements:
  # Apptainer as primary container runtime (preferred for HPC/SLURM)
  ApptainerRequirement:
    apptainerPull: docker://catgumag/alphafold:2.3.2
  ResourceRequirement:
    coresMin: 8
    ramMin: 32000  # 32 GB
  # GPU requirement for deep learning inference
  cwltool:CUDARequirement:
    cudaVersionMin: "11.0"
    cudaComputeCapability: "7.0"
    cudaDeviceCountMin: 1

hints:
  # Fallback Docker image if Apptainer unavailable
  - class: DockerRequirement
    dockerPull: catgumag/alphafold:2.3.2

# Executable in $PATH within container
baseCommand: run_alphafold.py

inputs:
  fasta_file:
    type: File
    inputBinding:
      prefix: --fasta_paths=
      separate: false
    doc: Input FASTA file containing protein sequence(s)

  output_dir:
    type: string
    default: output
    inputBinding:
      prefix: --output_dir=
      separate: false
    doc: Directory for output predictions

  model_preset:
    type:
      type: enum
      symbols:
        - monomer
        - monomer_casp14
        - monomer_ptm
        - multimer
    default: monomer
    inputBinding:
      prefix: --model_preset=
      separate: false
    doc: Model configuration preset

  db_preset:
    type:
      type: enum
      symbols:
        - full_dbs
        - reduced_dbs
    default: reduced_dbs
    inputBinding:
      prefix: --db_preset=
      separate: false
    doc: Database preset (reduced_dbs for faster search)

  max_template_date:
    type: string?
    inputBinding:
      prefix: --max_template_date=
      separate: false
    doc: Maximum template release date (YYYY-MM-DD)

  use_gpu:
    type: boolean
    default: true
    inputBinding:
      prefix: --use_gpu=
      separate: false
      valueFrom: '$(self ? "true" : "false")'
    doc: Enable GPU acceleration

outputs:
  predictions:
    type: Directory
    outputBinding:
      glob: $(inputs.output_dir)
    doc: Directory containing predicted structures (PDB files) and confidence scores

  ranked_structures:
    type: File[]
    outputBinding:
      glob: $(inputs.output_dir)/ranked_*.pdb
    doc: Ranked PDB structure predictions
