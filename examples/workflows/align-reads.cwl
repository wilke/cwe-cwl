#!/usr/bin/env cwl-runner
cwlVersion: v1.2
class: Workflow

label: Read Alignment Pipeline
doc: |
  Aligns paired-end reads to a reference genome and converts to sorted BAM.

requirements:
  SubworkflowFeatureRequirement: {}
  ScatterFeatureRequirement: {}

inputs:
  reference:
    type: File
    secondaryFiles:
      - .amb
      - .ann
      - .bwt
      - .pac
      - .sa
    doc: Reference genome with BWA index

  reads_1:
    type: File
    doc: Forward reads (FASTQ)

  reads_2:
    type: File?
    doc: Reverse reads (FASTQ, optional)

  sample_id:
    type: string
    doc: Sample identifier for read group

outputs:
  aligned_bam:
    type: File
    outputSource: sam_to_bam/bam_file
    doc: Sorted BAM file with index

  alignment_stats:
    type: File
    outputSource: flagstat/stats_file
    doc: Alignment statistics

steps:
  align:
    run: ../tools/bwa-mem.cwl
    in:
      reference: reference
      reads_1: reads_1
      reads_2: reads_2
      read_group:
        valueFrom: '@RG\tID:$(inputs.sample_id)\tSM:$(inputs.sample_id)\tPL:ILLUMINA'
      output_name:
        valueFrom: $(inputs.sample_id).sam
    out: [aligned_reads]

  sam_to_bam:
    run:
      class: CommandLineTool
      requirements:
        DockerRequirement:
          dockerPull: biocontainers/samtools:v1.9-4-deb_cv1
        ResourceRequirement:
          coresMin: 2
          ramMin: 4000
      hints:
        - class: ApptainerRequirement
          apptainerPull: docker://biocontainers/samtools:v1.9-4-deb_cv1
      baseCommand: samtools
      arguments:
        - sort
        - -@
        - $(runtime.cores)
        - -o
        - $(inputs.output_name)
      inputs:
        sam_file:
          type: File
          inputBinding:
            position: 1
        output_name:
          type: string
          default: sorted.bam
      outputs:
        bam_file:
          type: File
          secondaryFiles: [.bai]
          outputBinding:
            glob: $(inputs.output_name)
    in:
      sam_file: align/aligned_reads
      output_name:
        source: sample_id
        valueFrom: $(self).sorted.bam
    out: [bam_file]

  flagstat:
    run:
      class: CommandLineTool
      requirements:
        DockerRequirement:
          dockerPull: biocontainers/samtools:v1.9-4-deb_cv1
      hints:
        - class: ApptainerRequirement
          apptainerPull: docker://biocontainers/samtools:v1.9-4-deb_cv1
      baseCommand: [samtools, flagstat]
      inputs:
        bam_file:
          type: File
          inputBinding:
            position: 1
        output_name:
          type: string
          default: stats.txt
      stdout: $(inputs.output_name)
      outputs:
        stats_file:
          type: File
          outputBinding:
            glob: $(inputs.output_name)
    in:
      bam_file: sam_to_bam/bam_file
      output_name:
        source: sample_id
        valueFrom: $(self).flagstat.txt
    out: [stats_file]
