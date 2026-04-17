# CURRENT_STATE

LastUpdated: 2026-04-17T20:39:55+08:00

## Topic

GNN node classification on Cora dataset

## Literature

- [1] The RSNA Abdominal Traumatic Injury CT (RATIC) Dataset (https://arxiv.org/pdf/2405.19595v1)
  - The RSNA Abdominal Traumatic Injury CT (RATIC) dataset is the largest publicly available collection of adult abdominal CT studies annotated for traumatic injuries. This dataset includes 4,274 studies from 23 institutions across 14 countries...
- [2] The RSNA Lumbar Degenerative Imaging Spine Classification (LumbarDISC) Dataset (https://arxiv.org/pdf/2506.09162v1)
  - The Radiological Society of North America (RSNA) Lumbar Degenerative Imaging Spine Classification (LumbarDISC) dataset is the largest publicly available dataset of adult MRI lumbar spine examinations annotated for degenerative changes. The ...
- [3] Label-dependent Feature Extraction in Social Networks for Node Classification (https://arxiv.org/pdf/1303.0095v1)
  - A new method of feature extraction in the social network for within-network classification is proposed in the paper. The method provides new features calculated by combination of both: network structure information and class labels assigned...
- [4] GNN-MultiFix: Addressing the pitfalls for GNNs for multi-label node classification (https://arxiv.org/pdf/2411.14094v1)
  - Graph neural networks (GNNs) have emerged as powerful models for learning representations of graph data showing state of the art results in various tasks. Nevertheless, the superiority of these methods is usually supported by either evaluat...
- [5] Node Disjoint Multipath Routing Considering Link and Node Stability protocol: A characteristic Evaluation (https://arxiv.org/pdf/1002.1162v1)
  - Mobile Ad hoc Networks are highly dynamic networks. Quality of Service (QoS) routing in such networks is usually limited by the network breakage due to either node mobility or energy depletion of the mobile nodes. Also, to fulfill certain q...

## Code

```python
import math
import random

random.seed(0)

TOPIC = "GNN node classification on Cora dataset"

def make_data(n=400, d=6):
    X = []
    y = []
    w = [random.uniform(-1, 1) for _ in range(d)]
    b = ra