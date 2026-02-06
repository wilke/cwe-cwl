#!/usr/bin/env cwl-runner
cwlVersion: v1.2
class: CommandLineTool

label: BWA-MEM Sequence Aligner
doc: |
  Aligns sequence reads to a reference genome using BWA-MEM algorithm.
  Produces SAM-formatted output suitable for downstream variant calling.

requirements:
  DockerRequirement:
    dockerPull: biocontainers/bwa:v0.7.17_cv1
  ResourceRequirement:
    coresMin: 4
    ramMin: 8000  # 8 GB

hints:
  - class: ApptainerRequirement
    apptainerPull: docker://biocontainers/bwa:v0.7.17_cv1

# Executable must be in $PATH - no relative or absolute paths allowed
baseCommand: bwa
arguments:
  - mem
  - -t
  - $(runtime.cores)

inputs:
  reference:
    type: File
    inputBinding:
      position: 1
    secondaryFiles:
      - .amb
      - .ann
      - .bwt
      - .pac
      - .sa
    doc: Reference genome FASTA file with BWA index files

  reads_1:
    type: File
    inputBinding:
      position: 2
    doc: Forward reads (FASTQ format, may be gzipped)

  reads_2:
    type: File?
    inputBinding:
      position: 3
    doc: Reverse reads for paired-end sequencing (optional)

  read_group:
    type: string?
    inputBinding:
      prefix: -R
    doc: |
      Read group header line (e.g., '@RG\tID:sample1\tSM:sample1\tPL:ILLUMINA')

  output_name:
    type: string
    default: aligned.sam
    doc: Name for the output SAM file

stdout: $(inputs.output_name)

outputs:
  aligned_reads:
    type: File
    outputBinding:
      glob: $(inputs.output_name)
    doc: Aligned reads in SAM format
