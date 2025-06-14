import math
import matplotlib.pyplot as plt
import argparse

def parse_args():
    p = argparse.ArgumentParser(
        description="Plot a grid of mesh-network nodes."
    )
    p.add_argument(
        "--count", "-n",
        type=int,
        default=10,
        help="Number of nodes to place (default: 10)"
    )
    p.add_argument(
        "--area", "-a",
        type=float,
        default=9.0,
        help="Total area in km² (default: 9.0)"
    )
    return p.parse_args()

def main():
    args = parse_args()
    count = args.count
    area_km2 = args.area

    # Compute grid dimensions
    rows = math.ceil(math.sqrt(count))
    cols = rows
    side = math.sqrt(area_km2)
    spacing = side / (rows - 1)

    # Generate node coordinates
    coords = []
    idx = 0
    for r in range(rows):
        for c in range(cols):
            if idx >= count:
                break
            lat = r * spacing     # y-coordinate
            lng = c * spacing     # x-coordinate
            coords.append((lat, lng))
            idx += 1
        if idx >= count:
            break

    # Separate coordinates for plotting
    latitudes = [c[0] for c in coords]
    longitudes = [c[1] for c in coords]

    # Plot
    plt.figure()
    plt.scatter(longitudes, latitudes, marker='x')
    plt.xlabel('Longitude (km)')
    plt.ylabel('Latitude (km)')
    plt.title(f'Node Placement: {count} Nodes over {area_km2} km²')
    plt.grid(True)
    plt.show()

if __name__ == "__main__":
    main()
